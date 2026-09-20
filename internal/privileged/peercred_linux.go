//go:build linux

package privileged

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// PeerPolicy describes which local processes may use the root helper socket.
// UID equality alone is not sufficient on hosts where unrelated services share
// the panel account, so AllowedUnit additionally requires the connecting
// process to live inside the named systemd unit's cgroup (e.g. veil.service).
type PeerPolicy struct {
	// AllowedUID is the unprivileged account the panel runs as.
	AllowedUID uint32
	// AllowRoot additionally accepts uid 0 peers (tests, manual recovery).
	AllowRoot bool
	// AllowedUnit, when non-empty, requires a peer connecting with AllowedUID
	// to also appear in that unit's cgroup. Root peers (when allowed) are not
	// unit-bound; root can reach the helper regardless.
	AllowedUnit string
}

// panelUnitName is the systemd unit every authorized non-root helper peer must
// belong to. Internet-facing units run as veil-proxy and can never satisfy it.
const panelUnitName = "veil.service"

// socketDirMode is the traverse-only mode for the helper socket parent
// directory. root:root 0711 lets the panel reach /run/veil/helper.sock while
// nobody (including veil) can list or create entries inside it — replacing the
// directory or swapping the socket requires root (audit #514).
const socketDirMode = 0o711

// readPeerCgroup loads the cgroup membership of a peer pid. It is a test hook
// so unit binding can be exercised without real systemd cgroups.
var readPeerCgroup = func(pid int32) ([]byte, error) {
	return os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
}

// chownSocket is a test hook for socket filesystem ownership; sockets cannot be
// opened as files, so the chown goes through the path directly.
var chownSocket = os.Chown

func (s *Server) ServeUnix(ctx context.Context, path string, policy PeerPolicy) error {
	if err := validateSocketPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	// Missing parents are created traverse-only so the socket parent never
	// becomes group- or world-writable when the helper is started by hand
	// (audit #523).
	if err := os.MkdirAll(dir, socketDirMode); err != nil {
		return err
	}
	// The canonical socket parent used to be owned by the panel unit's
	// RuntimeDirectory (veil:veil), which let any veil-uid process replace the
	// helper socket. When serving as root on the packaged layout, normalize it
	// back to root:root 0711.
	if effectiveUID() == 0 && dir == filepath.Dir(DefaultSocketPath) {
		if err := chownSocket(dir, 0, 0); err != nil {
			return fmt.Errorf("normalize helper socket directory ownership: %w", err)
		}
		if err := os.Chmod(dir, socketDirMode); err != nil {
			return fmt.Errorf("normalize helper socket directory mode: %w", err)
		}
	}
	if info, err := os.Lstat(path); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(effectiveUID()) {
			return newError(ErrorForbiddenOperation, "existing helper socket is not owned by the helper user")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	if effectiveUID() == 0 && path == DefaultSocketPath {
		// Match the veil-helper.socket contract (root:veil 0660) so a manually
		// started helper on the packaged socket path is indistinguishable from
		// the systemd-managed one (audit #513). Custom socket paths carry no
		// packaged ownership contract, so they only get the restrictive mode
		// below. Failing to resolve the veil group must fail closed.
		gid, err := veilGroupGID()
		if err != nil {
			return fmt.Errorf("resolve veil group for helper socket: %w", err)
		}
		if err := chownSocket(path, 0, gid); err != nil {
			return fmt.Errorf("chgrp helper socket: %w", err)
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		return err
	}
	return s.serveUnixListener(ctx, listener, policy)
}

// veilGroupGID resolves the numeric gid of the veil group (the group the helper
// socket is shared with).
func veilGroupGID() (int, error) {
	u, err := lookupUser("veil")
	if err != nil {
		return 0, fmt.Errorf("resolve veil user: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse veil gid %q: %w", u.Gid, err)
	}
	return gid, nil
}

func (s *Server) serveUnixListener(ctx context.Context, listener *net.UnixListener, policy PeerPolicy) error {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if err := verifyPeer(conn, policy); err != nil {
			_ = conn.Close()
			continue
		}
		go s.ServeConn(ctx, conn)
	}
}

func verifyPeer(conn *net.UnixConn, policy PeerPolicy) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if controlErr != nil {
		return controlErr
	}
	if credential == nil {
		return errors.New("peer credentials unavailable")
	}
	if credential.Uid == policy.AllowedUID {
		if policy.AllowedUnit != "" && !peerInUnit(credential.Pid, policy.AllowedUnit) {
			return fmt.Errorf("peer uid %d pid %d is not running inside %s", credential.Uid, credential.Pid, policy.AllowedUnit)
		}
		return nil
	}
	if policy.AllowRoot && credential.Uid == 0 {
		return nil
	}
	return fmt.Errorf("peer uid %d is not authorized", credential.Uid)
}

// peerInUnit reports whether pid runs inside the named systemd unit. It works
// for both cgroup v2 (0::/path) and v1 (<h>:<ctrl>:<path>) layouts, matching
// the unit segment exactly so veil.service.evil cannot satisfy veil.service.
func peerInUnit(pid int32, unit string) bool {
	body, err := readPeerCgroup(pid)
	if err != nil {
		return false
	}
	want := "/system.slice/" + unit
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		if path == want || strings.HasSuffix(path, want) || strings.HasPrefix(path, want+"/") || strings.Contains(path, want+"/") {
			return true
		}
	}
	return false
}
