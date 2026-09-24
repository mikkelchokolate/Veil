//go:build linux && linuxintegration

package linuxintegration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// TestIntegrationHelperSocketAcceptsAllowedUIDAndDispatches covers UID-policy
// acceptance and dispatch only — the veil.service unit binding
// (PeerPolicy.AllowedUnit) cannot be exercised from a test process that is not
// inside that cgroup; the unit-boundary rejection path is covered by
// TestIntegrationHelperSocketRejectsProxyUID and the cgroup parser tests in
// internal/privileged.
func TestIntegrationHelperSocketAcceptsAllowedUIDAndDispatches(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "helper.sock")
	var calls atomic.Int32
	server := privileged.NewServer(privileged.NewLocalAdapter(privileged.Policy{
		ManagedUnits:    map[string]struct{}{"veil.service": {}},
		Artifacts:       map[string]privileged.ArtifactPath{},
		UpdateArtifacts: map[string]string{},
		FirewallRules:   map[string]struct{}{},
	}, privileged.Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, socketPath, privileged.PeerPolicy{AllowedUID: uint32(os.Getuid())})
	}()
	waitForPath(t, socketPath)

	if err := privileged.NewSocketClient(socketPath).RestartPanel(context.Background()); err != nil {
		t.Fatalf("restart through helper socket: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("executor calls=%d", calls.Load())
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode=%#o", info.Mode().Perm())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("helper did not stop")
	}
}

// TestIntegrationHelperSocketCanonicalLayout covers audits #513/#514/#523:
// starting the helper manually on the packaged layout must normalize
// /run/veil to root:root 0711 (so no veil-uid process can replace the socket)
// and deliver the socket itself as root:veil 0660.
func TestIntegrationHelperSocketCanonicalLayout(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("helper socket layout requires root")
	}
	if _, err := os.Lstat(privileged.DefaultSocketPath); err == nil {
		// Never skip on the packaged layout (issue #685): a skip is not
		// evidence. A live helper owning the socket means the test environment
		// is wrong — fail. A stale node with no listener is safe to remove so
		// the canonical-layout probe still executes.
		conn, dialErr := net.DialTimeout("unix", privileged.DefaultSocketPath, time.Second)
		if dialErr == nil {
			_ = conn.Close()
			t.Fatalf("real helper socket %s is already active — refusing to overwrite the live packaged layout", privileged.DefaultSocketPath)
		}
		if err := os.Remove(privileged.DefaultSocketPath); err != nil {
			t.Fatalf("remove stale helper socket node %s: %v", privileged.DefaultSocketPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat helper socket %s: %v", privileged.DefaultSocketPath, err)
	}
	veilUID, veilGID := requireVeilIdentity(t)
	dir := filepath.Dir(privileged.DefaultSocketPath)
	// Simulate the pre-fix layout: a veil-owned RuntimeDirectory that let any
	// veil-uid process replace the helper socket.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dir, veilUID, veilGID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	server := privileged.NewServer(privileged.NewLocalAdapter(privileged.Policy{
		ManagedUnits:    map[string]struct{}{"veil.service": {}},
		Artifacts:       map[string]privileged.ArtifactPath{},
		UpdateArtifacts: map[string]string{},
		FirewallRules:   map[string]struct{}{},
	}, privileged.Executor{
		RestartPanel: func(context.Context) error { return nil },
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, privileged.DefaultSocketPath, privileged.PeerPolicy{AllowedUID: uint32(veilUID), AllowRoot: true})
	}()
	waitForPath(t, privileged.DefaultSocketPath)
	defer func() {
		cancel()
		<-done
	}()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	dirStat := dirInfo.Sys().(*syscall.Stat_t)
	if dirStat.Uid != 0 || dirStat.Gid != 0 || dirInfo.Mode().Perm() != 0o711 {
		t.Fatalf("helper socket dir uid=%d gid=%d mode=%#o, want root:root 0711", dirStat.Uid, dirStat.Gid, dirInfo.Mode().Perm())
	}
	sockInfo, err := os.Stat(privileged.DefaultSocketPath)
	if err != nil {
		t.Fatal(err)
	}
	sockStat := sockInfo.Sys().(*syscall.Stat_t)
	if sockStat.Uid != 0 || int(sockStat.Gid) != veilGID || sockInfo.Mode().Perm() != 0o660 {
		t.Fatalf("helper socket uid=%d gid=%d mode=%#o, want root:veil 0660", sockStat.Uid, sockStat.Gid, sockInfo.Mode().Perm())
	}
	// The panel identity can still connect through the traverse-only dir.
	if err := privileged.NewSocketClient(privileged.DefaultSocketPath).RestartPanel(context.Background()); err != nil {
		t.Fatalf("root peer restart through canonical socket: %v", err)
	}
}

// helperProbeChildEnv marks the child process that connects to the helper
// socket under a dropped identity (audit #506/#511 coverage).
const helperProbeChildEnv = "VEIL_HELPER_PROBE_CHILD"

// TestIntegrationHelperSocketRejectsProxyUID proves the privilege boundary: a
// process running as veil-proxy — the internet-facing identity — connecting to
// a helper socket authorized for the panel uid must be refused before any
// executor dispatch, even though it can traverse the socket directory.
func TestIntegrationHelperSocketRejectsProxyUID(t *testing.T) {
	if os.Getenv(helperProbeChildEnv) == "1" {
		err := privileged.NewSocketClient(os.Getenv("VEIL_HELPER_PROBE_SOCK")).RestartPanel(context.Background())
		if err == nil {
			// Exit 0 signals the unauthorized call was accepted.
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "rejected as expected:", err)
		os.Exit(3)
	}
	if os.Geteuid() != 0 {
		t.Skip("peer boundary probe requires root")
	}
	veilUID, _ := requireVeilIdentity(t)
	proxyUID, proxyGID := requireProxyIdentity(t)

	// The probe must traverse the socket parent as veil-proxy: use a
	// traverse-only top-level directory, not the 0700 test tempdir.
	dir, err := os.MkdirTemp("", "veil-helper-probe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o711); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(dir, "helper.sock")
	server := privileged.NewServer(privileged.NewLocalAdapter(privileged.Policy{
		ManagedUnits:    map[string]struct{}{"veil.service": {}},
		Artifacts:       map[string]privileged.ArtifactPath{},
		UpdateArtifacts: map[string]string{},
		FirewallRules:   map[string]struct{}{},
	}, privileged.Executor{
		RestartPanel: func(context.Context) error { return nil },
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, socketPath, privileged.PeerPolicy{AllowedUID: uint32(veilUID), AllowedUnit: "veil.service"})
	}()
	waitForPath(t, socketPath)
	defer func() {
		cancel()
		<-done
	}()

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary lives under the root-owned 0700 go-build tree, so
	// veil-proxy cannot exec it in place. Copy it into the traversable probe
	// dir (0711) as a 0755 file and re-exec the copy instead.
	probeBinary := filepath.Join(dir, "probe.test")
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(probeBinary, binaryBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(probeBinary, "-test.run=^TestIntegrationHelperSocketRejectsProxyUID$", "-test.count=1", "-test.v")
	command.Env = append(os.Environ(),
		helperProbeChildEnv+"=1",
		"VEIL_HELPER_PROBE_SOCK="+socketPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(proxyUID), Gid: uint32(proxyGID)},
	}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("veil-proxy peer was accepted by the helper socket:\n%s", output)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 3 {
		t.Fatalf("probe exit=%v (want rejection exit 3):\n%s", err, output)
	}
}

func requireProxyIdentity(t *testing.T) (int, int) {
	t.Helper()
	account, err := user.Lookup("veil-proxy")
	if err != nil {
		t.Fatalf("veil-proxy account is required by privilege-boundary CI: %v", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return uid, gid
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path was not created: %s", path)
}
