package privileged

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The client object persists after the helper socket disappears (detached
// alias, stopped helper). Reachable must reflect the socket, not the client.
func TestSocketClientReachableReflectsSocketPresence(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")

	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	client := NewSocketClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Reachable(ctx); err != nil {
		t.Fatalf("Reachable against a live socket: %v", err)
	}

	// Detached alias: the listener survives on its other name but the dialed
	// path no longer resolves.
	if err := os.Remove(sock); err != nil {
		t.Fatalf("remove socket alias: %v", err)
	}
	if err := client.Reachable(ctx); err == nil {
		t.Fatal("Reachable must fail once the socket path is gone")
	}
	_ = listener.Close()

	// Stale socket file: path exists but nothing accepts connections.
	stale := filepath.Join(dir, "stale.sock")
	staleListener, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatalf("listen stale: %v", err)
	}
	_ = staleListener.Close()
	staleClient := NewSocketClient(stale)
	if err := staleClient.Reachable(ctx); err == nil {
		t.Fatal("Reachable must fail when the socket file accepts nothing")
	}
}
