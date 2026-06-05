//go:build linux

package privileged

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func (s *Server) ServeUnix(ctx context.Context, path string, allowedUID uint32, allowRoot bool) error {
	if err := validateSocketPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
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
	if err := os.Chmod(path, 0o660); err != nil {
		return err
	}
	return s.serveUnixListener(ctx, listener, allowedUID, allowRoot)
}

func (s *Server) serveUnixListener(ctx context.Context, listener *net.UnixListener, allowedUID uint32, allowRoot bool) error {
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
		if err := verifyPeerUID(conn, allowedUID, allowRoot); err != nil {
			_ = conn.Close()
			continue
		}
		go s.ServeConn(ctx, conn)
	}
}

func verifyPeerUID(conn *net.UnixConn, allowedUID uint32, allowRoot bool) error {
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
	if credential.Uid == allowedUID || (allowRoot && credential.Uid == 0) {
		return nil
	}
	return fmt.Errorf("peer uid %d is not authorized", credential.Uid)
}
