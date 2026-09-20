package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
)

// #539: with durable apply tracking disabled (no StatePath) the panel cannot
// prove the runtime matches desired state — /api/apply/state must report
// "untracked", never a false-green "synced".
func TestApplyStateUntrackedWhenTrackingDisabled(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := v1Request(t, r, http.MethodGet, "/api/apply/state", "")
	if w.Code != http.StatusOK {
		t.Fatalf("apply state: %d %s", w.Code, w.Body.String())
	}
	var view applyStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != apply.StateUntracked {
		t.Fatalf("untracked apply state = %q, want %q", view.State, apply.StateUntracked)
	}
	if view.State == apply.StateSynced {
		t.Fatal("tracking-disabled state must never claim synced")
	}
}

// #534/#535/#546: a recovery_pending job is still active — the derived state
// must be "recovering" (not synced), surface the job's lastError, report it
// as the active job, and count it as the last failed job.
func TestApplyStateRecoveryPendingIsHonest(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)

	job := apply.Job{
		ID:              "job-recovery-1",
		DesiredRevision: 1,
		Status:          apply.StatusPending,
		Trigger:         "mutation",
		CreatedAt:       time.Now().Unix(),
	}
	if err := state.applyJobs.Create(job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := state.applyJobs.MarkStatus(job.ID, apply.StatusRecoveryPending, "ROLLBACK_FAILED", "firewall rollback failed"); err != nil {
		t.Fatalf("mark recovery_pending: %v", err)
	}

	w := v1Request(t, r, http.MethodGet, "/api/apply/state", "")
	if w.Code != http.StatusOK {
		t.Fatalf("apply state: %d %s", w.Code, w.Body.String())
	}
	var view applyStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != apply.StateRecovering {
		t.Fatalf("state = %q, want %q while recovery_pending job is active", view.State, apply.StateRecovering)
	}
	if view.ActiveJobID != job.ID {
		t.Fatalf("activeJobId = %q, want %q", view.ActiveJobID, job.ID)
	}
	if view.LastFailedJobID != job.ID {
		t.Fatalf("lastFailedJobId = %q, want %q (recovery_pending must count as failed, #546)", view.LastFailedJobID, job.ID)
	}
	if view.LastError == nil || view.LastError.Message != "firewall rollback failed" {
		t.Fatalf("lastError = %+v, want the recovery_pending job's error (#535)", view.LastError)
	}
}

// #543: a terminal failed job with desired==applied is unresolved evidence —
// state must be degraded and lastError surfaced, not "synced".
func TestApplyStateFailedEqualRevisionIsDegraded(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)

	// desired==applied directly: BumpDesired then MarkApplied.
	revNum, err := state.applyRevisions.BumpDesired()
	if err != nil {
		t.Fatalf("bump desired: %v", err)
	}
	if err := state.applyRevisions.MarkApplied(revNum); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	rev, err := state.applyRevisions.Get()
	if err != nil || rev.Desired == 0 || rev.Desired != rev.Applied {
		t.Fatalf("expected converged revisions, got %+v err=%v", rev, err)
	}

	job := apply.Job{
		ID:              "job-failed-eq",
		DesiredRevision: rev.Desired,
		Status:          apply.StatusPending,
		Trigger:         "manual",
		CreatedAt:       time.Now().Unix() + 1,
	}
	if err := state.applyJobs.Create(job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := state.applyJobs.Finish(job.ID, apply.StatusFailed, "APPLY_ERROR", "boom"); err != nil {
		t.Fatalf("finish failed: %v", err)
	}

	w := v1Request(t, r, http.MethodGet, "/api/apply/state", "")
	if w.Code != http.StatusOK {
		t.Fatalf("apply state: %d %s", w.Code, w.Body.String())
	}
	var view applyStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != apply.StateDegraded {
		t.Fatalf("state = %q, want %q for failed equal-revision job", view.State, apply.StateDegraded)
	}
	if view.LastError == nil || view.LastError.Message != "boom" {
		t.Fatalf("lastError = %+v, want failed job's error", view.LastError)
	}
}

// #536: when an apply was attempted but produced no durable job, the mutation
// response must still carry the real outcome — success must not be forced.
func TestMutationOutcomeWithoutJobPreservesFailure(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	defer func() { _ = state.Close() }()

	obj := map[string]any{"name": "x"}
	state.mergeOutcomeInto(obj, autoApplyOutcome{attempted: true, success: false})
	if obj["success"] != false {
		t.Fatalf("success = %v, want false for failed attempted apply with no job", obj["success"])
	}
	obj = map[string]any{"name": "x"}
	state.mergeOutcomeInto(obj, autoApplyOutcome{attempted: true, success: true})
	if obj["success"] != true {
		t.Fatalf("success = %v, want true for succeeded apply", obj["success"])
	}
	obj = map[string]any{"name": "x"}
	state.mergeOutcomeInto(obj, autoApplyOutcome{attempted: false})
	if obj["success"] != true {
		t.Fatalf("success = %v, want true when no apply was required", obj["success"])
	}
}

// #545: convergeRevisionForSideEffect must not report RuntimeConverged from
// revision equality alone — when the live unit probe fails it falls through
// to a real apply; only healthy runtime evidence short-circuits.
func TestConvergeRevisionRequiresRuntimeEvidence(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)

	w := postJSON(t, r, "/api/inbounds", `{"name":"conv-hy","protocol":"hysteria2","transport":"udp","port":14434,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	rev, err := state.applyRevisions.Get()
	if err != nil || rev.Desired == 0 || rev.Desired != rev.Applied {
		t.Fatalf("expected converged revision, got %+v err=%v", rev, err)
	}

	// Healthy runtime evidence -> converged fast path, no service actions.
	serviceHealthChecker = func(name string) ServiceHealthResult {
		return ServiceHealthResult{Name: name, Healthy: true}
	}
	var actions [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		actions = append(actions, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}
	res, err := state.convergeRevisionForSideEffect(context.Background(), rev.Desired)
	if err != nil {
		t.Fatalf("converge (healthy): %v", err)
	}
	if !res.Success || res.Disposition != apply.ApplyDispositionRuntimeConverged {
		t.Fatalf("converge (healthy) = %+v, want RuntimeConverged success", res)
	}
	if len(actions) != 0 {
		t.Fatalf("converged fast path must not run service actions, ran %v", actions)
	}

	// Unhealthy unit -> no convergence claim; a real apply must run (it may
	// itself fail at the workflow health gate - both outcomes are honest; what
	// must not happen is a RuntimeConverged claim).
	serviceHealthChecker = func(name string) ServiceHealthResult {
		return ServiceHealthResult{Name: name, Healthy: false}
	}
	res, err = state.convergeRevisionForSideEffect(context.Background(), rev.Desired)
	if err == nil && res.Disposition == apply.ApplyDispositionRuntimeConverged {
		t.Fatalf("converge (unhealthy) claimed RuntimeConverged without evidence: %+v", res)
	}
	if len(actions) == 0 {
		t.Fatalf("unhealthy probe must fall through to a real apply; no service actions ran (err=%v)", err)
	}
}

// #544: a retry whose apply run fails returns 200 with an explicit
// success:false and error — status-only clients must not read it as success.
func TestApplyJobRetryReportsExecutionFailure(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// Fail config validation: the job fails before any runtime mutation, so it
	// lands terminal "failed" (not recovery_pending) and a retry is allowed —
	// then fails the same way.
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: false, Error: "forced invalid"})
		}
		return out
	}

	w := postJSON(t, r, "/api/inbounds", `{"name":"retry-hy","protocol":"hysteria2","transport":"udp","port":14433,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	var createResp struct {
		ApplyJob *apply.Job `json:"applyJob"`
		Success  bool       `json:"success"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if createResp.Success {
		t.Fatal("failed apply must report success=false on the mutation (#536)")
	}
	if createResp.ApplyJob == nil {
		t.Fatalf("failed apply must carry the job record: %s", w.Body.String())
	}

	w = v1Request(t, r, http.MethodPost, "/api/apply/jobs/"+createResp.ApplyJob.ID+"/retry", "")
	if w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	var retryResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &retryResp); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if retryResp.Success {
		t.Fatalf("retry with failed run must report success=false: %s", w.Body.String())
	}
	if retryResp.Error == "" {
		t.Fatalf("retry failure must carry an error message: %s", w.Body.String())
	}
}
