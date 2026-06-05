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

	if err := verifyPeerUID(serverConn, uint32(os.Getuid()), false); err != nil {
		t.Fatalf("configured UID rejected: %v", err)
	}
	if err := verifyPeerUID(serverConn, uint32(os.Getuid()+1), false); err == nil {
		t.Fatal("unexpected UID accepted")
	}
}

func TestLinuxServeUnixRejectsPeerBeforeExecution(t *testing.T) {
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
		done <- server.ServeUnix(ctx, socketPath, uint32(os.Getuid()+1), false)
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
	_, _ = conn.Write([]byte(`{"version":1,"requestId":"peer","operation":"restart_panel","restartPanel":{}}`))
	_ = conn.Close()
	time.Sleep(25 * time.Millisecond)
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
