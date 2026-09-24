//go:build linux

package privileged

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLinuxPeerCredentialsAcceptConfiguredUIDAndRejectAnother(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()

	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer client.Close()
	serverConn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("accept unix: %v", err)
	}
	defer serverConn.Close()

	if err := verifyPeer(serverConn, PeerPolicy{AllowedUID: uint32(os.Getuid())}); err != nil {
		t.Fatalf("configured UID rejected: %v", err)
	}
	if err := verifyPeer(serverConn, PeerPolicy{AllowedUID: uint32(os.Getuid() + 1)}); err == nil {
		t.Fatal("unexpected UID accepted")
	}
}

// A peer with the panel uid but outside the panel unit must be rejected when
// the policy binds authorization to veil.service (audit #506).
func TestLinuxPeerCredentialsRejectsUIDOutsideAllowedUnit(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "peer-unit.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()

	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer client.Close()
	serverConn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("accept unix: %v", err)
	}
	defer serverConn.Close()

	oldRead := readPeerCgroup
	defer func() { readPeerCgroup = oldRead }()
	policy := PeerPolicy{AllowedUID: uint32(os.Getuid()), AllowedUnit: "veil.service"}

	readPeerCgroup = func(int32) ([]byte, error) {
		return []byte("0::/user.slice/user-1000.slice/app.slice/some-other.service\n"), nil
	}
	if err := verifyPeer(serverConn, policy); err == nil {
		t.Fatal("peer outside veil.service was accepted")
	}

	readPeerCgroup = func(int32) ([]byte, error) {
		return []byte("0::/system.slice/veil.service\n"), nil
	}
	if err := verifyPeer(serverConn, policy); err != nil {
		t.Fatalf("peer inside veil.service rejected: %v", err)
	}

	// A similarly-named unit must not satisfy the check.
	readPeerCgroup = func(int32) ([]byte, error) {
		return []byte("0::/system.slice/veil.service.evil\n"), nil
	}
	if err := verifyPeer(serverConn, policy); err == nil {
		t.Fatal("peer in veil.service.evil was accepted")
	}
}

func TestLinuxServeUnixRejectsPeerBeforeExecution(t *testing.T) {
	// Run as a non-root helper so the socket ownership normalization branch is
	// skipped; peer rejection is exercised below.
	oldUID := effectiveUID
	defer func() { effectiveUID = oldUID }()
	effectiveUID = func() int { return os.Getuid() + 1 }
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketPath := filepath.Join(t.TempDir(), "helper.sock")
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, socketPath, PeerPolicy{AllowedUID: uint32(os.Getuid() + 1)})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial helper: %v", err)
	}
	if _, err := conn.Write([]byte(`{"version":1,"requestId":"peer","operation":"restart_panel","restartPanel":{}}`)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// Rejected peers get no response — the helper closes the connection right
	// after verifyPeer fails. Reading to EOF is the barrier proving the peer
	// check ran before dispatch; a fixed sleep could pass without one.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, readErr := conn.Read(make([]byte, 1)); n != 0 || readErr == nil {
		_ = conn.Close()
		t.Fatalf("rejected peer received a response or stayed connected: n=%d err=%v", n, readErr)
	}
	_ = conn.Close()
	if calls.Load() != 0 {
		t.Fatalf("executor called for rejected peer: %d", calls.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeUnix did not stop after cancellation")
	}
}
