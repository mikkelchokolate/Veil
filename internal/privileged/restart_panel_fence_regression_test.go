package privileged

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestartPanelDispatchAttachesFenceToContext(t *testing.T) {
	var got RestartPanelRequest
	var called bool
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(ctx context.Context) error {
			called = true
			got, _ = RestartPanelRequestFromContext(ctx)
			return nil
		},
	}))
	fence := FenceToken{
		Owner: "apply-owner", Generation: 4, OperationID: "restart-op",
		LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	}
	response := servePipeRequest(t, server, RequestEnvelope{
		Version: ProtocolVersion, RequestID: "restart-fence",
		Operation:    OperationRestartPanel,
		RestartPanel: &RestartPanelRequest{Fence: fence},
	})
	if !response.OK || response.Error != nil {
		t.Fatalf("expected ok response, got %+v", response)
	}
	if !called {
		t.Fatal("RestartPanel executor was not called")
	}
	if got.Fence != fence {
		t.Fatalf("dispatcher dropped restart fence: %+v", got.Fence)
	}
}

func TestRestartPanelSocketRoundTripRequiresFence(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "helper.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	binaryPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(binaryPath, []byte("panel-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy()
	policy.FencePath = ""
	policy.RequireFence = true
	var showCalls int
	executor := NewProductionExecutor(ProductionConfig{
		BinaryPath: binaryPath,
		RunCommand: func(_ context.Context, command []string, _ time.Duration) (string, error) {
			if len(command) > 1 && command[0] == "systemctl" && command[1] == "show" {
				showCalls++
				return fmt.Sprintf("LoadState=loaded\nActiveState=active\nSubState=running\nMainPID=%d\nExecMainStartTimestampMonotonic=%d\n", showCalls, showCalls), nil
			}
			return "", nil
		},
	})
	server := NewServer(NewLocalAdapter(policy, executor))
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.ServeConn(context.Background(), conn)
		}
	}()

	client := NewSocketClient(socketPath)
	if err := client.RestartPanel(context.Background()); err == nil {
		t.Fatal("missing fence was accepted")
	} else if !strings.Contains(err.Error(), "fencing token") && !strings.Contains(err.Error(), "fencing context") {
		t.Fatalf("missing fence error = %v", err)
	}

	fence := FenceToken{
		Owner: "panel", Generation: 1, OperationID: "restart-op",
		LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	}
	ctx := ContextWithRestartPanelRequest(context.Background(), RestartPanelRequest{Fence: fence})
	if err := client.RestartPanel(ctx); err != nil {
		t.Fatalf("fenced restart_panel: %v", err)
	}

	expired := fence
	expired.LeaseExpiresAt = time.Now().Add(-time.Minute).Unix()
	expired.Generation = 2
	expired.OperationID = "expired-op"
	expiredCtx := ContextWithRestartPanelRequest(context.Background(), RestartPanelRequest{Fence: expired})
	if err := client.RestartPanel(expiredCtx); err == nil {
		t.Fatal("expired fence was accepted")
	}
}
