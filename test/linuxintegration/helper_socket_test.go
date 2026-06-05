//go:build linux && linuxintegration

package linuxintegration

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestIntegrationHelperSocketAuthenticatesPeerAndDispatches(t *testing.T) {
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
		done <- server.ServeUnix(ctx, socketPath, uint32(os.Getuid()), false)
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
