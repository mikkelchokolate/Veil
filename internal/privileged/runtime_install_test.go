package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

func TestRuntimeInstallRejectsUnapprovedNamesBeforeExecution(t *testing.T) {
	calls := 0
	adapter := NewLocalAdapter(testPolicy(t), Executor{InstallRuntimes: func(context.Context, RuntimeInstallRequest) error { calls++; return nil }})
	for _, names := range [][]string{nil, {""}, {"../../bin/sh"}, {"https://example/runtime"}, {"caddy"}, {"warp;id"}, {" WARP "}, {"warp", "warp"}, {"warp", "not-allowed"}, {"warp", "hysteria2", "naiveproxy", "mieru", "olcrtc", "warp"}} {
		if err := adapter.InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: names}); err == nil {
			t.Errorf("accepted %q", names)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached installer %d times", calls)
	}
}

func TestRuntimeInstallRequiresCurrentFenceAndBoundsExecution(t *testing.T) {
	policy := testPolicy(t)
	policy.RequireFence = true
	calls := 0
	adapter := NewLocalAdapter(policy, Executor{InstallRuntimes: func(ctx context.Context, _ RuntimeInstallRequest) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > mutationOperationBudget {
			t.Error("runtime install has no bounded mutation deadline")
		}
		return nil
	}})
	request := RuntimeInstallRequest{Names: []string{"warp"}}
	if err := adapter.InstallRuntimes(t.Context(), request); err == nil {
		t.Fatal("unfenced install accepted")
	}
	request.Fence = validRegressionFenceToken("owner", 2)
	request.Fence.LeaseExpiresAt = time.Now().Add(-time.Minute).Unix()
	if err := adapter.InstallRuntimes(t.Context(), request); err == nil {
		t.Fatal("expired fence accepted")
	}
	request.Fence = validRegressionFenceToken("owner", 2)
	if err := adapter.InstallRuntimes(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	request.Fence = validRegressionFenceToken("owner", 1)
	if err := adapter.InstallRuntimes(t.Context(), request); err == nil {
		t.Fatal("stale fence accepted")
	}
	if calls != 1 {
		t.Fatalf("installer called %d times", calls)
	}
}

func TestProductionRuntimeInstallUsesOnlyPinnedManagedDescriptors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper-owned directory validation requires Unix ownership")
	}
	var got []string
	root := t.TempDir()
	executor := NewProductionExecutor(ProductionConfig{
		IPCertDir: filepath.Join(root, "panel"),
		InstallRuntime: func(_ context.Context, opts runtimeinstall.Options, descriptor runtimeinstall.Runtime) runtimeinstall.Result {
			if opts.BinDir != runtimeinstall.DefaultBinDir() || opts.CaddyCacheDir != filepath.Join(root, ".runtime-build") {
				t.Fatalf("unexpected install paths: %+v", opts)
			}
			if descriptor.Version == "" || descriptor.Version == "latest" || descriptor.Integrity == "" {
				t.Fatalf("unverified descriptor: %+v", descriptor)
			}
			got = append(got, descriptor.Name)
			return runtimeinstall.Result{Installed: true}
		},
	})
	names := []string{"naiveproxy", "hysteria2", "mieru", "olcrtc", "warp"}
	if err := executor.InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: names}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, names) {
		t.Fatalf("installed %v, want %v", got, names)
	}
}

func TestProductionRuntimeInstallRejectsUnsafeBuildCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper-owned directory validation requires Unix ownership")
	}
	for _, scenario := range []string{"symlink", "public-mode"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			cache := filepath.Join(root, ".runtime-build")
			if scenario == "symlink" {
				if err := os.Symlink(t.TempDir(), cache); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(cache, 0o755); err != nil {
				t.Fatal(err)
			}
			calls := 0
			executor := NewProductionExecutor(ProductionConfig{IPCertDir: filepath.Join(root, "panel"), InstallRuntime: func(context.Context, runtimeinstall.Options, runtimeinstall.Runtime) runtimeinstall.Result {
				calls++
				return runtimeinstall.Result{Installed: true}
			}})
			if err := executor.InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: []string{"olcrtc"}}); err == nil {
				t.Fatal("unsafe build cache accepted")
			}
			if calls != 0 {
				t.Fatal("installer reached unsafe build cache")
			}
		})
	}
}

func TestRuntimeInstallServerContractAndProgressWhileRunning(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	executor := NewProductionExecutor(ProductionConfig{InstallRuntime: func(_ context.Context, opts runtimeinstall.Options, descriptor runtimeinstall.Runtime) runtimeinstall.Result {
		opts.Progress("downloading", descriptor.Name)
		close(entered)
		<-release
		return runtimeinstall.Result{Installed: true}
	}})
	adapter := NewLocalAdapter(testPolicy(t), executor)
	server := NewServer(adapter)
	request := RuntimeInstallRequest{Names: []string{"hysteria2"}, Fence: validRegressionFenceToken("owner", 1)}
	done := make(chan error, 1)
	go func() { done <- adapter.InstallRuntimes(t.Context(), request) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("installer did not start")
	}
	response := servePipeRequest(t, server, RequestEnvelope{Version: ProtocolVersion, RequestID: "progress", Operation: OperationRuntimeInstallStatus, RuntimeInstallStatus: &RuntimeInstallStatusRequest{OperationID: request.Fence.OperationID}})
	if !response.OK {
		t.Fatalf("progress: %+v", response)
	}
	var status RuntimeInstallStatusResult
	if err := json.Unmarshal(response.Result, &status); err != nil {
		t.Fatal(err)
	}
	if status.OperationID != request.Fence.OperationID || status.Stage != "downloading" || status.Runtime != "hysteria2" || status.UpdatedAt == 0 {
		t.Fatalf("progress: %+v", status)
	}
	unknown, err := adapter.RuntimeInstallStatus(t.Context(), RuntimeInstallStatusRequest{OperationID: "different-operation"})
	if err != nil || unknown.Stage != "" {
		t.Fatalf("unrelated progress leaked: %+v %v", unknown, err)
	}
	select {
	case <-done:
		t.Fatal("installer returned before release")
	default:
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	finished, err := adapter.RuntimeInstallStatus(t.Context(), RuntimeInstallStatusRequest{OperationID: request.Fence.OperationID})
	if err != nil || finished.Stage != "complete" {
		t.Fatalf("final progress: %+v %v", finished, err)
	}
}

func TestRuntimeInstallSocketDispatchAndValidation(t *testing.T) {
	calls := 0
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{InstallRuntimes: func(_ context.Context, request RuntimeInstallRequest) error {
		calls++
		if !reflect.DeepEqual(request.Names, []string{"warp"}) {
			t.Errorf("names changed: %v", request.Names)
		}
		return nil
	}}))
	response := servePipeRequest(t, server, RequestEnvelope{Version: ProtocolVersion, RequestID: "install", Operation: OperationRuntimeInstall, RuntimeInstall: &RuntimeInstallRequest{Names: []string{"warp"}}})
	if !response.OK || calls != 1 {
		t.Fatalf("install dispatch: %+v calls=%d", response, calls)
	}
	for _, raw := range []string{
		`{"version":1,"requestId":"bad","operation":"runtime_install"}`,
		`{"version":1,"requestId":"bad","operation":"runtime_install","runtimeInstall":{"names":["warp"],"url":"https://evil.test"}}`,
		`{"version":1,"requestId":"bad","operation":"runtime_install","runtimeInstall":{"names":["warp"],"binDir":"/etc"}}`,
		`{"version":1,"requestId":"bad","operation":"runtime_install","runtimeInstall":{"names":["/bin/sh"]}}`,
		`{"version":1,"requestId":"bad","operation":"runtime_install","runtimeInstall":{"names":["warp"]},"restartPanel":{}}`,
	} {
		if result := servePipeRaw(t, server, raw); result.OK || result.Error == nil {
			t.Fatalf("accepted unsafe payload %s: %+v", raw, result)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid payload invoked executor, calls=%d", calls)
	}
	path := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		if request.Operation != OperationRuntimeInstall || request.RuntimeInstall == nil {
			t.Errorf("socket request: %+v", request)
		}
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: []byte(`{}`)}
	})
	client := NewSocketClient(path)
	if err := client.InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: []string{"warp"}}); err != nil {
		t.Fatal(err)
	}
	if got := client.operationTimeout(OperationRuntimeInstall); got != mutationOperationBudget {
		t.Fatalf("install budget: %v", got)
	}
	if got := client.operationTimeout(OperationRuntimeInstallStatus); got != defaultOperationBudget {
		t.Fatalf("progress budget: %v", got)
	}
	statusPath := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		if request.Operation != OperationRuntimeInstallStatus || request.RuntimeInstallStatus == nil || request.RuntimeInstallStatus.OperationID != "apply-1" {
			t.Errorf("status socket request: %+v", request)
		}
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: []byte(`{"operationId":"apply-1","stage":"building","runtime":"olcrtc","updatedAt":1}`)}
	})
	status, err := NewSocketClient(statusPath).RuntimeInstallStatus(t.Context(), RuntimeInstallStatusRequest{OperationID: "apply-1"})
	if err != nil || status.Stage != "building" || status.Runtime != "olcrtc" {
		t.Fatalf("status socket result: %+v %v", status, err)
	}
}

func TestRuntimeInstallPropagatesFailureAndMissingExtensions(t *testing.T) {
	adapter := NewLocalAdapter(testPolicy(t), Executor{InstallRuntimes: func(context.Context, RuntimeInstallRequest) error { return errors.New("download failed") }})
	if err := adapter.InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: []string{"warp"}}); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("failure lost: %v", err)
	}
	if err := NewLocalAdapter(testPolicy(t), Executor{}).InstallRuntimes(t.Context(), RuntimeInstallRequest{Names: []string{"warp"}}); err == nil {
		t.Fatal("missing executor accepted")
	}
}

func TestRuntimeInstallProgressRetainsOnlyCurrentOperation(t *testing.T) {
	progress := &runtimeInstallProgress{now: time.Now}
	progress.start("first")
	progress.update("first", "building", "olcrtc")
	progress.start("second")
	progress.update("first", "complete", "")
	if old := progress.status("first"); old.Stage != "" {
		t.Fatalf("old progress retained: %+v", old)
	}
	if current := progress.status("second"); current.Stage != "checking" {
		t.Fatalf("old operation replaced current progress: %+v", current)
	}
}
