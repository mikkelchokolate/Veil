//go:build linux

package privileged

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeSystemdAdoptsListenerAndHandlesRequest(t *testing.T) {
	original := newSystemdUnixListener
	oldReadPeerCgroup := readPeerCgroup
	defer func() {
		newSystemdUnixListener = original
		readPeerCgroup = oldReadPeerCgroup
	}()
	// ServeSystemd binds peers to veil.service; stand in for the test process.
	// The cgroup is a variable so the second probe can impersonate a peer in a
	// different unit — accepting it would prove AllowedUnit was not injected.
	var peerCgroup atomic.Value
	peerCgroup.Store([]byte("0::/system.slice/veil.service\n"))
	readPeerCgroup = func(int32) ([]byte, error) {
		return peerCgroup.Load().([]byte), nil
	}

	path := filepath.Join(t.TempDir(), "activated.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	listener.SetUnlinkOnClose(false)

	called := false
	listenerReady := make(chan struct{})
	newSystemdUnixListener = func() (*net.UnixListener, error) {
		close(listenerReady)
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
		done <- server.ServeSystemd(ctx, PeerPolicy{AllowedUID: uint32(os.Getuid())})
	}()

	select {
	case <-listenerReady:
	case <-time.After(time.Second):
		t.Fatal("server did not initialize systemd listener")
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial activated listener: %v", err)
	}
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

	if !called {
		t.Fatal("executor was not called")
	}

	// Wrong-unit peer with the right UID must be refused. verifyPeer treats an
	// empty AllowedUnit as "no unit check", so this probe fails if
	// ServeSystemd ever stops auto-binding AllowedUnit = veil.service.
	peerCgroup.Store([]byte("0::/system.slice/sshd.service\n"))
	rejected, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial activated listener for wrong-unit peer: %v", err)
	}
	_ = json.NewEncoder(rejected).Encode(RequestEnvelope{
		Version:      ProtocolVersion,
		RequestID:    "wrong-unit-req",
		Operation:    OperationRestartPanel,
		RestartPanel: &RestartPanelRequest{},
	})
	_ = rejected.SetReadDeadline(time.Now().Add(2 * time.Second))
	var rejectedResponse ResponseEnvelope
	decodeErr := json.NewDecoder(rejected).Decode(&rejectedResponse)
	_ = rejected.Close()
	if decodeErr == nil && rejectedResponse.OK {
		t.Fatal("wrong-unit peer was served; ServeSystemd must bind AllowedUnit to veil.service")
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
	if err := server.ServeSystemd(context.Background(), PeerPolicy{}); err == nil {
		t.Fatal("expected listener error to propagate")
	}
}
