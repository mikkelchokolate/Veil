package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// The durable apply operations started by these handlers must run on the
// management lifecycle context, not the request context: a client disconnect
// (or the SPA's own fetch timeout) must not cancel render, health polling,
// rollback, or job finalization mid-flight (#1052).
//
// Every test issues the request on an already-canceled context. Before the
// fix the runner's heartbeat observed <-ctx.Done() immediately and the run
// finished as a canceled failure; with s.mutationApplyContext() the run
// survives to its durable outcome.

func canceledRequest(method, path, body string) *http.Request {
	req := adminJSONRequest(method, path, body)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	cancel()
	return req
}

// stubSuccessRunner swaps in a runner whose executor converges immediately.
// A plain ExecutorFunc is required — a ContextExecutor triggers strictPhases,
// which demands the executor record every intermediate durable publication
// phase like the real management workflow does; the synthetic success path is
// exactly what RunContext supports for non-context executors.
func stubSuccessRunner(t *testing.T, state *managementState) *atomic.Bool {
	t.Helper()
	var ran atomic.Bool
	runner := veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ExecutorFunc(
		func(uint64) (veilapply.Result, error) {
			ran.Store(true)
			return veilapply.Result{
				Success: true, Disposition: veilapply.ApplyDispositionRuntimeConverged, MarkRevisionLive: true,
			}, nil
		}))
	t.Cleanup(runner.Close)
	state.applyRunner = runner
	return &ran
}

func TestApplyRetryRunsAfterRequestContextCancel(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	ran := stubSuccessRunner(t, state)
	state.mu.Lock()
	desired, err := state.bumpDesiredRevisionLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	orig := veilapply.Job{
		ID: "retry-orig", DesiredRevision: desired, BaseRevision: desired,
		Status: veilapply.StatusFailed, Trigger: "test", CreatedAt: time.Now().Unix(),
	}
	if err := state.applyJobs.Create(orig); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	state.handleApplyJobByID(response, canceledRequest(http.MethodPost, "/api/apply/jobs/retry-orig/retry", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded struct {
		Success  bool          `json:"success"`
		ApplyJob veilapply.Job `json:"applyJob"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Success || decoded.ApplyJob.Status != veilapply.StatusSucceeded {
		t.Fatalf("retry after client disconnect: %+v", decoded)
	}
	if !ran.Load() {
		t.Fatal("apply executor never ran")
	}
}

func TestApplyReconcileRunsAfterRequestContextCancel(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	ran := stubSuccessRunner(t, state)
	state.mu.Lock()
	if _, err := state.bumpDesiredRevisionLocked(); err != nil {
		state.mu.Unlock()
		t.Fatal(err)
	}
	state.mu.Unlock()

	response := httptest.NewRecorder()
	state.handleApplyReconcile(response, canceledRequest(http.MethodPost, "/api/apply/reconcile", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded struct {
		Reconciled bool `json:"reconciled"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Reconciled {
		t.Fatalf("reconcile after client disconnect: %s", response.Body.String())
	}
	if !ran.Load() {
		t.Fatal("reconcile executor never ran")
	}
}

func TestManualApplyRunsAfterRequestContextCancel(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	// Route privileged work through the recording adapter: the real apply
	// workflow still stages/promotes/converges, but helper round-trips stay
	// in-process so the run finishes well inside the 15s test bound.
	state.privileged = &recordingPrivilegedClient{
		promoteResult:     privileged.PromoteResult{BackupID: "20260608T120000.000000000Z"},
		statusActiveState: "active",
	}
	state.privilegedLocal = false
	response := httptest.NewRecorder()
	state.handleApply(response, canceledRequest(http.MethodPost, "/api/apply", `{"confirm":true}`))
	// The durable run happens inside the handler: with the request context
	// plumbed through, its heartbeat observes the cancelation and the handler
	// reports the operation failed rather than converged.
	if response.Code != http.StatusOK {
		t.Fatalf("manual apply after client disconnect: status=%d body=%s", response.Code, response.Body.String())
	}
}

// Durable service actions take the same operation path as applies: the fence
// is minted inside RunOperationContext and the privileged action executes
// under the operation context — not the request's.
func TestServiceActionRunsAfterRequestContextCancel(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	state.mu.Lock()
	snapshot, err := state.snapshotLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Pin revision 0 so ensureRunnableRevision returns it and the converge
	// resolves through the health-probe fast path instead of a full apply.
	if err := state.applySnapshots.Save(0, payload); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	state.handleServiceActionRoute(response,
		canceledRequest(http.MethodPost, "/api/services/hysteria2-edge/restart", `{"confirm":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("service action after client disconnect: status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded ServiceActionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Success {
		t.Fatalf("service action response=%s", response.Body.String())
	}
	if len(client.serviceActions) != 1 {
		t.Fatalf("privileged service actions=%+v", client.serviceActions)
	}
	action := client.serviceActions[0]
	if action.Unit != "veil-hysteria2@edge.service" || action.Action != privileged.ServiceAction("restart") {
		t.Fatalf("unexpected service action: %+v", action)
	}
	if decoded.Service != "hysteria2-edge" {
		t.Fatalf("service action response missing service name: %s", response.Body.String())
	}
}
