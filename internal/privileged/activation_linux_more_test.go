//go:build linux

package privileged

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestServeSystemdAdoptsListenerAndHandlesRequest(t *testing.T) {
	original := newSystemdUnixListener
	defer func() { newSystemdUnixListener = original }()

	path := filepath.Join(t.TempDir(), "activated.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	listener.SetUnlinkOnClose(false)

	called := false
	newSystemdUnixListener = func() (*net.UnixListener, error) {
		return listener, nil
	}

	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			called = true
			return nil
		},
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeSystemd(ctx, 0, false)
	}()

	// Wait for the server goroutine to start accepting.
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("unix", path, 50*time.Millisecond)
		if err == nil {
			_ = json.NewEncoder(conn).Encode(RequestEnvelope{
				Version:      ProtocolVersion,
				RequestID:    "systemd-req",
				Operation:    OperationRestartPanel,
				RestartPanel: &RestartPanelRequest{},
			})
			var response ResponseEnvelope
			_ = json.NewDecoder(conn).Decode(&response)
			_ = conn.Close()
			if !response.OK {
				t.Fatalf("expected ok response, got %+v", response)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not accept connections")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !called {
		t.Fatal("executor was not called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSystemd did not stop after context cancellation")
	}
}

func TestServeSystemdPropagatesListenerError(t *testing.T) {
	original := newSystemdUnixListener
	defer func() { newSystemdUnixListener = original }()

	newSystemdUnixListener = func() (*net.UnixListener, error) {
		return nil, net.ErrClosed
	}
	server := NewServer(nil)
	if err := server.ServeSystemd(context.Background(), 0, false); err == nil {
		t.Fatal("expected listener error to propagate")
	}
}
