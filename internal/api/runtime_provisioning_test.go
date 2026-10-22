package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type runtimeInstallClient struct {
	privileged.Client
	install func(context.Context, privileged.RuntimeInstallRequest) error
}

type runtimeStatusClient struct {
	privileged.Client
	status privileged.RuntimeInstallStatusResult
}

func (c runtimeStatusClient) RuntimeInstallStatus(context.Context, privileged.RuntimeInstallStatusRequest) (privileged.RuntimeInstallStatusResult, error) {
	return c.status, nil
}

func TestRuntimeProgressObserverPreservesEventTimeAndStops(t *testing.T) {
	updated := time.Now().Unix() - 5
	state := &managementState{applyProgress: &applyProgressTracker{}, privileged: runtimeStatusClient{status: privileged.RuntimeInstallStatusResult{
		OperationID: "runtime-test", Stage: "downloading", Runtime: "mieru", UpdatedAt: updated,
	}}}
	operation := apply.ContextWithFence(context.Background(), apply.Fence{Owner: "test", Generation: 1, OperationID: "runtime-test"})
	ctx := NewManagementApplyContextWithContext(state, operation)
	ctx.beginProgress()
	stop := ctx.observeRuntimeProgress()
	defer ctx.clearProgress()
	deadline := time.Now().Add(3 * time.Second)
	for state.applyProgress.snapshot().Stage != "downloading" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	view := state.applyProgress.snapshot()
	if view.Stage != "downloading" || view.Runtime != "mieru" || view.UpdatedAt != updated {
		t.Fatalf("missing real helper phase/event time: %+v", view)
	}
	ctx.reportProgress("planning", "")
	if state.applyProgress.snapshot().Stage != "planning" {
		t.Fatal("stopped observer overwrote next apply phase")
	}
}

type runtimeDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (r *runtimeDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadline = deadline
	return nil
}

func TestRuntimeMutationWriteBudgetIsScoped(t *testing.T) {
	for _, test := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/inbounds", true},
		{http.MethodPut, "/api/warp", true},
		{http.MethodPost, "/api/apply/jobs/id/retry", true},
		{http.MethodGet, "/api/inbounds", false},
		{http.MethodPost, "/api/auth/login", false},
		{http.MethodPost, "/api/apply-unrelated", false},
	} {
		writer := &runtimeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		allowRuntimeMutationTime(writer, httptest.NewRequest(test.method, test.path, nil))
		if writer.deadline.IsZero() == test.want {
			t.Fatalf("unexpected write budget for %s %s", test.method, test.path)
		}
		if test.want && (time.Until(writer.deadline) < 16*time.Minute || time.Until(writer.deadline) > 17*time.Minute) {
			t.Fatalf("runtime write budget is not bounded at 17 minutes: %s", writer.deadline)
		}
	}
}

func (c runtimeInstallClient) InstallRuntimes(ctx context.Context, request privileged.RuntimeInstallRequest) error {
	return c.install(ctx, request)
}

func TestRequiredRuntimeNames(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings Settings
		inbounds []Inbound
		warp     WarpConfig
		want     []string
	}{
		{name: "panel only", want: []string{}},
		{name: "disabled inbound", inbounds: []Inbound{{Protocol: "mieru"}}, want: []string{}},
		{name: "one protocol", inbounds: []Inbound{{Protocol: "hysteria2", Enabled: true}}, want: []string{"hysteria2"}},
		{name: "deduplicated", inbounds: []Inbound{{Protocol: "mieru", Enabled: true}, {Protocol: "mieru", Enabled: true}}, want: []string{"mieru"}},
		{name: "olcrtc without warp", inbounds: []Inbound{{Protocol: "olcrtc", Enabled: true}}, want: []string{"olcrtc"}},
		{name: "warp only", warp: WarpConfig{Enabled: true}, want: []string{"warp"}},
		{name: "panel caddy", settings: Settings{PanelAccess: "caddy"}, want: []string{"naiveproxy"}},
		{name: "hysteria certificate", inbounds: []Inbound{{Protocol: "hysteria2", Enabled: true, ProtocolFields: map[string]any{"domain": "hy.example.com"}}}, want: []string{"hysteria2", "naiveproxy"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := requiredRuntimeNames(test.settings, test.inbounds, test.warp)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("selected %v, want %v (error: %v)", got, test.want, err)
			}
		})
	}
	if _, err := requiredRuntimeNames(Settings{}, []Inbound{{Protocol: "unknown", Enabled: true}}, WarpConfig{}); err == nil {
		t.Fatal("unknown protocol accepted")
	}
}

func TestApplyInstallsSelectedRuntimesBeforePlanning(t *testing.T) {
	old := provisionApplyRuntimes
	provisionApplyRuntimes = installApplyRuntimes
	t.Cleanup(func() { provisionApplyRuntimes = old })
	router, state := newApplyTrackedRouterWithState(t)
	installed := 0
	state.privileged = runtimeInstallClient{Client: state.privileged, install: func(ctx context.Context, request privileged.RuntimeInstallRequest) error {
		installed++
		if !reflect.DeepEqual(request.Names, []string{"hysteria2"}) {
			t.Errorf("unexpected runtime set %v", request.Names)
		}
		fence, ok := apply.FenceFromContext(ctx)
		if !ok || request.Fence.OperationID != fence.OperationID || request.Fence.Generation == 0 {
			t.Errorf("runtime installation was not fenced: %+v", request.Fence)
		}
		return errors.New("download unavailable")
	}}
	response := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"lazy-hy","protocol":"hysteria2","transport":"udp","port":28341,"enabled":true}`)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"success":false`) {
		t.Fatalf("failed download must leave a retryable committed mutation: %d %s", response.Code, response.Body.String())
	}
	jobs, err := state.applyJobs.List(1)
	if err != nil || len(jobs) != 1 || jobs[0].Status != apply.StatusFailed || !strings.Contains(jobs[0].ErrorMessage, "download unavailable") {
		t.Fatalf("failed download was not recorded: %+v, %v", jobs, err)
	}
	if installed != 1 {
		t.Fatalf("install calls=%d, want 1", installed)
	}
	state.privileged = runtimeInstallClient{Client: state.privileged, install: func(context.Context, privileged.RuntimeInstallRequest) error {
		installed++
		return nil
	}}
	retry := v1Request(t, router, http.MethodPost, "/api/apply/jobs/"+jobs[0].ID+"/retry", "")
	if retry.Code != http.StatusOK || installed != 2 {
		t.Fatalf("retry after failed download: %d %s, installs=%d", retry.Code, retry.Body.String(), installed)
	}
}

func TestApplyProgressDoesNotBlockPanelOrLeakAcrossOperations(t *testing.T) {
	old := provisionApplyRuntimes
	provisionApplyRuntimes = installApplyRuntimes
	t.Cleanup(func() { provisionApplyRuntimes = old })
	router, state := newApplyTrackedRouterWithState(t)
	started, release := make(chan struct{}), make(chan struct{})
	state.privileged = runtimeInstallClient{Client: state.privileged, install: func(context.Context, privileged.RuntimeInstallRequest) error {
		close(started)
		<-release
		return nil
	}}
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(
			`{"name":"lazy-progress","protocol":"hysteria2","transport":"udp","port":28342,"enabled":true}`)))
		finished <- response
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime installation never started")
	}
	defer func() { close(release); <-finished }()
	status := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/apply/state", nil))
		status <- response
	}()
	select {
	case response := <-status:
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"stage":"installing_runtime"`) {
			t.Fatalf("missing in-flight progress: %d %s", response.Code, response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panel status blocked behind runtime download")
	}
	preview := NewManagementApplyContextWithContext(state, context.Background())
	preview.reportProgress("planning", "")
	if got := state.applyProgress.snapshot(); got.Stage != "installing_runtime" {
		t.Fatal("an unfenced preview overwrote active progress")
	}
}

func TestStagedApplyDoesNotInstallRuntimes(t *testing.T) {
	old := provisionApplyRuntimes
	calls := 0
	provisionApplyRuntimes = func(ManagementApplyContext) error { calls++; return errors.New("must not install") }
	t.Cleanup(func() { provisionApplyRuntimes = old })
	state := &managementState{applyRoot: t.TempDir(), applyProgress: &applyProgressTracker{}}
	_, _, _ = NewApplyWorkflow(NewManagementApplyContextWithContext(state, context.Background())).RunLocked(ApplyRequest{Confirm: true})
	if calls != 0 || state.applyProgress.snapshot() != nil {
		t.Fatal("stage-only apply downloaded runtimes or published live progress")
	}
}
