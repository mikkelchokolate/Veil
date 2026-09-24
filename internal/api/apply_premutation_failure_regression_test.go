package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/applyhistory"
)

// #967: a PromoteStagedConfigs failure that happens BEFORE any live mutation —
// here the privileged helper is unavailable, so nothing was promoted and no
// mutation marker was recorded — must finalize the apply job as `failed`,
// never `recovery_pending`. Before the fix every promote error was stamped
// MutationStarted+Ambiguous, which parked the job in recovery_pending even
// though recovery has nothing to roll back.
func TestApplyPreMutationFailureFinalizesFailedNotRecoveryPending(t *testing.T) {
	r, _, state := newApplyTrackedRouterAt(t, t.TempDir())
	t.Cleanup(func() { _ = state.Close() })

	// The privileged helper is unavailable, so promotion cannot even start.
	state.mu.Lock()
	state.privileged = nil
	state.privilegedLocal = false
	state.mu.Unlock()

	// An enabled hysteria2 inbound produces promotable generated artifacts, so
	// the apply reaches promoteStagedConfigs and fails at the missing helper —
	// the pre-mutation branch (#967).
	body := strings.NewReader(`{"name":"hy2-premut","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success  bool      `json:"success"`
		ApplyJob apply.Job `json:"applyJob"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode mutation response: %v", err)
	}
	if resp.Success {
		t.Fatalf("apply must not report success when promotion never ran: %s", w.Body.String())
	}
	if resp.ApplyJob.ID == "" {
		t.Fatalf("expected durable applyJob in mutation response: %s", w.Body.String())
	}
	if resp.ApplyJob.Status == apply.StatusRecoveryPending {
		t.Fatalf("pre-mutation failure must never park the job in %q — nothing mutated", apply.StatusRecoveryPending)
	}
	if resp.ApplyJob.Status != apply.StatusFailed {
		t.Fatalf("pre-mutation failure must finalize job as %q, got %q", apply.StatusFailed, resp.ApplyJob.Status)
	}
	if !strings.Contains(resp.ApplyJob.ErrorMessage, "privileged helper is unavailable") {
		t.Fatalf("job error should name the pre-mutation cause, got %q", resp.ApplyJob.ErrorMessage)
	}

	// The durable job store agrees: terminal failed, not recovery_pending.
	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+resp.ApplyJob.ID, nil))
	if wl.Code != http.StatusOK {
		t.Fatalf("get job: %d %s", wl.Code, wl.Body.String())
	}
	var job apply.Job
	if err := json.NewDecoder(wl.Body).Decode(&job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.Status != apply.StatusFailed {
		t.Fatalf("durable job status = %q, want %q (never %q)", job.Status, apply.StatusFailed, apply.StatusRecoveryPending)
	}

	// History records a failed, non-ambiguous "staged" entry: the runtime was
	// never touched, so the outcome is proven — ambiguity would be a lie.
	wh := httptest.NewRecorder()
	r.ServeHTTP(wh, httptest.NewRequest(http.MethodGet, "/api/apply/history?success=false", nil))
	if wh.Code != http.StatusOK {
		t.Fatalf("apply history: %d %s", wh.Code, wh.Body.String())
	}
	var entries []applyhistory.ApplyHistoryEntry
	if err := json.NewDecoder(wh.Body).Decode(&entries); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("pre-mutation failure must still record a history entry: %s", wh.Body.String())
	}
	latest := entries[len(entries)-1]
	if latest.Success {
		t.Fatalf("history entry for failed apply must carry success=false: %+v", latest)
	}
	if latest.Ambiguous || latest.Stage == "ambiguous" {
		t.Fatalf("pre-mutation failure is not ambiguous — nothing mutated: %+v", latest)
	}
	if latest.Stage != "staged" {
		t.Fatalf("pre-mutation failure must record stage %q, got %q", "staged", latest.Stage)
	}
	if latest.LiveApplied || latest.ServicesApplied || latest.Applied {
		t.Fatalf("pre-mutation failure must not claim live/services/applied: %+v", latest)
	}
}
