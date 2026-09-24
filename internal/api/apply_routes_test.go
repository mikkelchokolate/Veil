package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// newApplyTrackedRouter builds a router backed by a real StatePath so the
// durable apply subsystem (SQLite revisions + jobs) is active, with the
// service action runner and staged validator stubbed to succeed.
func newApplyTrackedRouter(t *testing.T) (http.Handler, *[][]string) {
	t.Helper()
	router, calls, state := newApplyTrackedRouterAt(t, t.TempDir())
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Errorf("close apply-tracked management state: %v", err)
		}
	})
	return router, calls
}

// newApplyTrackedRouterAt builds an apply-tracked router over an explicit
// state directory so tests can close the state and rebuild a second router —
// a real management-plane restart against the same durable veil.db. The
// caller owns the returned state (it is not closed by cleanup here).
func newApplyTrackedRouterAt(t *testing.T, dir string) (http.Handler, *[][]string, *managementState) {
	t.Helper()
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origHealth := serviceHealthChecker
	origAutoApply := autoApplyAfterMutation
	origFirewall := currentFirewallApplier()
	t.Cleanup(func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		serviceHealthChecker = origHealth
		autoApplyAfterMutation = origAutoApply
		swapFirewallApplier(origFirewall)
	})
	// Unit tests must never touch the host firewall: the local applier shells
	// out to ufw, which fails outright for a non-root test process (CI) and
	// would mutate the host ruleset as root.
	swapFirewallApplier(&fakeFirewallApplier{})
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: true})
		}
		return out
	}
	var calls [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		calls = append(calls, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	autoApplyAfterMutation = true

	statePath := filepath.Join(dir, "state.json")
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
			t.Fatalf("write state: %v", err)
		}
	}
	r, reloader := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: dir})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	return r, &calls, state
}

func TestApplyStateTracksDesiredVsApplied(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// Mutate settings: desired should bump and applied should follow after a
	// successful auto-apply.
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", body))
	if w.Code != http.StatusOK {
		t.Fatalf("settings put: %d %s", w.Code, w.Body.String())
	}

	ws := httptest.NewRecorder()
	r.ServeHTTP(ws, httptest.NewRequest(http.MethodGet, "/api/apply/state", nil))
	if ws.Code != http.StatusOK {
		t.Fatalf("apply state: %d", ws.Code)
	}
	var state applyStateResponse
	if err := json.NewDecoder(ws.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.DesiredRevision < 1 {
		t.Fatalf("expected desired revision >=1, got %+v", state)
	}
	// Successful auto-apply should converge applied to desired.
	if state.AppliedRevision != state.DesiredRevision {
		t.Fatalf("expected applied==desired after successful apply, got %+v", state)
	}
	if state.State != apply.StateSynced {
		t.Fatalf("expected synced state, got %q", state.State)
	}
}

func TestMutationResponseIncludesRevisionAndApplyJob(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	body := strings.NewReader(`{"name":"hy2-rev","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["revision"]; !ok {
		t.Fatalf("response missing revision: %v", resp)
	}
	if _, ok := resp["applyJob"]; !ok {
		t.Fatalf("response missing applyJob: %v", resp)
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp["success"])
	}
	// The mutated object must still be top-level (backward compatible).
	if resp["name"] != "hy2-rev" {
		t.Fatalf("expected object name top-level, got %v", resp["name"])
	}
}

func TestApplyJobsListAndGet(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/settings", body))

	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil))
	if wl.Code != http.StatusOK {
		t.Fatalf("jobs list: %d", wl.Code)
	}
	var list struct {
		Items []apply.Job `json:"items"`
	}
	if err := json.NewDecoder(wl.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatalf("expected at least one apply job")
	}
	id := list.Items[0].ID

	wg := httptest.NewRecorder()
	r.ServeHTTP(wg, httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+id, nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("get job: %d", wg.Code)
	}
	var job apply.Job
	if err := json.NewDecoder(wg.Body).Decode(&job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.ID != id {
		t.Fatalf("expected job %s, got %s", id, job.ID)
	}
}

func TestApplyJobHistorySurvivesRouterRestart(t *testing.T) {
	dir := t.TempDir()
	r1, _, state1 := newApplyTrackedRouterAt(t, dir)
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`)
	w1 := httptest.NewRecorder()
	r1.ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/api/settings", body))
	if w1.Code != http.StatusOK {
		t.Fatalf("settings put: %d %s", w1.Code, w1.Body.String())
	}

	wl := httptest.NewRecorder()
	r1.ServeHTTP(wl, httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil))
	if wl.Code != http.StatusOK {
		t.Fatalf("jobs list before restart: %d %s", wl.Code, wl.Body.String())
	}
	var before struct {
		Items []apply.Job `json:"items"`
	}
	if err := json.NewDecoder(wl.Body).Decode(&before); err != nil {
		t.Fatalf("decode jobs before restart: %v", err)
	}
	if len(before.Items) == 0 {
		t.Fatal("no apply jobs before restart")
	}
	orig := before.Items[0]

	ws := httptest.NewRecorder()
	r1.ServeHTTP(ws, httptest.NewRequest(http.MethodGet, "/api/apply/state", nil))
	var stateBefore applyStateResponse
	if err := json.NewDecoder(ws.Body).Decode(&stateBefore); err != nil {
		t.Fatalf("decode state before restart: %v", err)
	}

	// A real restart: close the management state (SQLite handle + runner) and
	// build a brand-new router over the same state directory.
	if err := state1.Close(); err != nil {
		t.Fatalf("close first management state: %v", err)
	}
	r2, _, state2 := newApplyTrackedRouterAt(t, dir)
	t.Cleanup(func() { _ = state2.Close() })

	wl2 := httptest.NewRecorder()
	r2.ServeHTTP(wl2, httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil))
	if wl2.Code != http.StatusOK {
		t.Fatalf("jobs list after restart: %d %s", wl2.Code, wl2.Body.String())
	}
	var after struct {
		Items []apply.Job `json:"items"`
	}
	if err := json.NewDecoder(wl2.Body).Decode(&after); err != nil {
		t.Fatalf("decode jobs after restart: %v", err)
	}
	var found *apply.Job
	for i := range after.Items {
		if after.Items[i].ID == orig.ID {
			found = &after.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("job %s missing from durable history after restart: %s", orig.ID, wl2.Body.String())
	}
	if found.Status != orig.Status || found.DesiredRevision != orig.DesiredRevision {
		t.Fatalf("job %s mutated across restart: before=%+v after=%+v", orig.ID, orig, *found)
	}
	if len(after.Items) != len(before.Items) {
		t.Fatalf("job count changed across restart: before=%d after=%d", len(before.Items), len(after.Items))
	}

	wg := httptest.NewRecorder()
	r2.ServeHTTP(wg, httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+orig.ID, nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("get job %s after restart: %d %s", orig.ID, wg.Code, wg.Body.String())
	}

	ws2 := httptest.NewRecorder()
	r2.ServeHTTP(ws2, httptest.NewRequest(http.MethodGet, "/api/apply/state", nil))
	var stateAfter applyStateResponse
	if err := json.NewDecoder(ws2.Body).Decode(&stateAfter); err != nil {
		t.Fatalf("decode state after restart: %v", err)
	}
	if stateAfter.DesiredRevision != stateBefore.DesiredRevision ||
		stateAfter.AppliedRevision != stateBefore.AppliedRevision {
		t.Fatalf("revisions changed across restart: before=%+v after=%+v", stateBefore, stateAfter)
	}
	if stateAfter.State != stateBefore.State {
		t.Fatalf("system state changed across restart: before=%q after=%q", stateBefore.State, stateAfter.State)
	}
}

func TestApplyJobRetryCreatesNewJob(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/settings", body))

	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil))
	var list struct {
		Items []apply.Job `json:"items"`
	}
	_ = json.NewDecoder(wl.Body).Decode(&list)
	if len(list.Items) == 0 {
		t.Fatalf("no jobs to retry")
	}
	orig := list.Items[0]

	wr := httptest.NewRecorder()
	r.ServeHTTP(wr, httptest.NewRequest(http.MethodPost, "/api/apply/jobs/"+orig.ID+"/retry", nil))
	if wr.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", wr.Code, wr.Body.String())
	}
	var resp struct {
		Success  bool      `json:"success"`
		ApplyJob apply.Job `json:"applyJob"`
		Revision struct {
			Desired uint64 `json:"desired"`
			Applied uint64 `json:"applied"`
			State   string `json:"state"`
		} `json:"revision"`
	}
	if err := json.NewDecoder(wr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	// The success envelope is explicit: status-only clients must see
	// success=true, the new job, and the post-run revisions — not infer
	// success from the HTTP status alone.
	if !resp.Success {
		t.Fatalf("retry success envelope missing success=true: %s", wr.Body.String())
	}
	if resp.ApplyJob.ID == orig.ID {
		t.Fatalf("retry must create a NEW job, not rewrite %s", orig.ID)
	}
	if resp.ApplyJob.Status != apply.StatusSucceeded {
		t.Fatalf("retry job status=%q, want %q", resp.ApplyJob.Status, apply.StatusSucceeded)
	}
	if resp.ApplyJob.DesiredRevision != orig.DesiredRevision {
		t.Fatalf("retry should target same revision %d, got %d", orig.DesiredRevision, resp.ApplyJob.DesiredRevision)
	}
	if resp.ApplyJob.Trigger != "retry" {
		t.Fatalf("retry job trigger=%q, want retry", resp.ApplyJob.Trigger)
	}
	if resp.Revision.Desired == 0 || resp.Revision.Desired != resp.Revision.Applied {
		t.Fatalf("retry revisions=%+v, want desired==applied>0", resp.Revision)
	}
	if resp.Revision.State != apply.StateSynced {
		t.Fatalf("retry revision state=%q, want %q", resp.Revision.State, apply.StateSynced)
	}

	// The original job must be preserved untouched — retry never rewrites
	// history.
	wg := httptest.NewRecorder()
	r.ServeHTTP(wg, httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+orig.ID, nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("get original job after retry: %d %s", wg.Code, wg.Body.String())
	}
	var preserved apply.Job
	if err := json.NewDecoder(wg.Body).Decode(&preserved); err != nil {
		t.Fatalf("decode preserved job: %v", err)
	}
	if preserved.Status != orig.Status ||
		!reflect.DeepEqual(preserved.FinishedAt, orig.FinishedAt) ||
		preserved.ErrorCode != orig.ErrorCode {
		t.Fatalf("retry rewrote the original job: before=%+v after=%+v", orig, preserved)
	}

	// Both jobs are now in durable history.
	wl2 := httptest.NewRecorder()
	r.ServeHTTP(wl2, httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil))
	var after struct {
		Items []apply.Job `json:"items"`
	}
	if err := json.NewDecoder(wl2.Body).Decode(&after); err != nil {
		t.Fatalf("decode jobs after retry: %v", err)
	}
	seen := map[string]bool{}
	for _, item := range after.Items {
		seen[item.ID] = true
	}
	if !seen[orig.ID] || !seen[resp.ApplyJob.ID] {
		t.Fatalf("history must contain both original %s and retry %s: %s", orig.ID, resp.ApplyJob.ID, wl2.Body.String())
	}
}

func TestApplyReconcileWhenSyncedIsNoop(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/settings", body))

	wr := httptest.NewRecorder()
	r.ServeHTTP(wr, httptest.NewRequest(http.MethodPost, "/api/apply/reconcile", nil))
	if wr.Code != http.StatusOK {
		t.Fatalf("reconcile: %d %s", wr.Code, wr.Body.String())
	}
	if !strings.Contains(wr.Body.String(), `"reconciled":false`) {
		t.Fatalf("expected no-op reconcile when synced, got %s", wr.Body.String())
	}
}

func TestApplyEndpointsRequireAuth(t *testing.T) {
	origToken := ""
	_ = origToken
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	// A static token makes the API require auth.
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: dir, AuthToken: "secret-token"})
	for _, path := range []string{"/api/apply/state", "/api/apply/jobs"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 without token, got %d", path, w.Code)
		}
	}
}
