package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

// viewerRoleRequest stamps the viewer role into the request context exactly
// like the auth middleware does for a non-admin session.
func viewerRoleRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKeyRole, "viewer"))
}

func adminRoleRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKeyRole, "admin"))
}

// newViewerSanitizationState builds a managementState with a live durable
// apply subsystem (SQLite-backed job store) for read-path tests.
func newViewerSanitizationState(t *testing.T) *managementState {
	t.Helper()
	db := testdb.Open(t)
	state := &managementState{db: db, applyRoot: t.TempDir()}
	state.applyRevisions = apply.NewRevisionStore(db)
	state.applyJobs = apply.NewJobStore(db)
	state.applyRunner = apply.NewRunner(state.applyRevisions, state.applyJobs, apply.ExecutorFunc(func(uint64) (apply.Result, error) {
		return apply.Result{Success: true}, nil
	}))
	t.Cleanup(func() { state.applyRunner.Close() })
	return state
}

// #1065: apply job error text and operation detail embed privileged
// subprocess output (stderr may contain rendered secrets). Viewers must
// receive the sanitized copy; admins keep the raw diagnostics.
func TestApplyJobEndpointsRedactPrivilegedOutputForViewers(t *testing.T) {
	state := newViewerSanitizationState(t)
	const jobID = "job-secret"
	if err := state.applyJobs.Create(apply.Job{
		ID: jobID, DesiredRevision: 1, Status: apply.StatusFailed, Trigger: "manual",
		CreatedAt: 1,
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := state.applyJobs.Finish(jobID, apply.StatusFailed, "RENDER_FAILED",
		"sing-box check failed: password: hunter2\nAuthorization: Bearer abc123TOKEN"); err != nil {
		t.Fatalf("finish job: %v", err)
	}
	if err := state.applyJobs.SetOperations(jobID, []apply.OperationResult{{
		Type: "render", Target: "sing-box", Success: false,
		Detail: "wrote /etc/veil/config.json password: hunter2",
	}}); err != nil {
		t.Fatalf("set operations: %v", err)
	}

	mux := http.NewServeMux()
	state.registerApplyRoutes(mux)

	// Viewer: list must not contain the raw credential.
	wv := httptest.NewRecorder()
	mux.ServeHTTP(wv, viewerRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/jobs", nil)))
	if wv.Code != http.StatusOK {
		t.Fatalf("viewer jobs list status=%d", wv.Code)
	}
	if strings.Contains(wv.Body.String(), "hunter2") || strings.Contains(wv.Body.String(), "abc123TOKEN") {
		t.Fatalf("viewer job list leaked privileged output: %s", wv.Body.String())
	}
	if !strings.Contains(wv.Body.String(), "[REDACTED]") {
		t.Fatalf("viewer job list not sanitized: %s", wv.Body.String())
	}

	// Viewer: single-job read must redact error + operation detail.
	wvj := httptest.NewRecorder()
	mux.ServeHTTP(wvj, viewerRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+jobID, nil)))
	if wvj.Code != http.StatusOK {
		t.Fatalf("viewer job get status=%d", wvj.Code)
	}
	if strings.Contains(wvj.Body.String(), "hunter2") {
		t.Fatalf("viewer job detail leaked privileged output: %s", wvj.Body.String())
	}
	var viewed apply.Job
	if err := json.NewDecoder(wvj.Body).Decode(&viewed); err != nil {
		t.Fatalf("decode viewer job: %v", err)
	}
	if len(viewed.Operations) != 1 || !strings.Contains(viewed.Operations[0].Detail, "[REDACTED]") {
		t.Fatalf("operation detail not sanitized for viewer: %+v", viewed.Operations)
	}

	// Admin: the raw diagnostic text must survive — sanitization is
	// viewer-only, not a silent loss of operator evidence.
	wa := httptest.NewRecorder()
	mux.ServeHTTP(wa, adminRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/jobs/"+jobID, nil)))
	if !strings.Contains(wa.Body.String(), "hunter2") {
		t.Fatalf("admin lost the raw job diagnostics: %s", wa.Body.String())
	}
}

// #1065: GET /api/apply/state surfaces the latest job error verbatim; for a
// viewer role it must be redacted.
func TestApplyStateRedactsLastErrorForViewers(t *testing.T) {
	state := newViewerSanitizationState(t)
	if err := state.applyJobs.Create(apply.Job{
		ID: "job-fail", DesiredRevision: 1, Status: apply.StatusFailed, Trigger: "manual", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := state.applyJobs.Finish("job-fail", apply.StatusFailed, "HEALTH_CHECK_FAILED",
		"hysteria2 health check failed: password: hunter2"); err != nil {
		t.Fatalf("finish job: %v", err)
	}

	mux := http.NewServeMux()
	state.registerApplyRoutes(mux)

	wv := httptest.NewRecorder()
	mux.ServeHTTP(wv, viewerRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/state", nil)))
	if wv.Code != http.StatusOK {
		t.Fatalf("viewer state status=%d", wv.Code)
	}
	var viewResp applyStateResponse
	if err := json.NewDecoder(wv.Body).Decode(&viewResp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if viewResp.LastError == nil || strings.Contains(viewResp.LastError.Message, "hunter2") {
		t.Fatalf("viewer lastError leaked privileged output: %+v", viewResp.LastError)
	}
	if !strings.Contains(viewResp.LastError.Message, "[REDACTED]") {
		t.Fatalf("viewer lastError not sanitized: %+v", viewResp.LastError)
	}

	wa := httptest.NewRecorder()
	mux.ServeHTTP(wa, adminRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/state", nil)))
	if !strings.Contains(wa.Body.String(), "hunter2") {
		t.Fatalf("admin lastError was sanitized away: %s", wa.Body.String())
	}
}

// #1065: apply history entries carry validation/service/health-check output;
// the viewer path must redact every embedded subprocess field.
func TestApplyHistoryRedactsSubprocessOutputForViewers(t *testing.T) {
	state := newViewerSanitizationState(t)
	response := model.ApplyResponse{
		Applied: true,
		Plan: model.ApplyPlanResponse{
			Valid:  false,
			Errors: []string{"render failed password: hunter2"},
			Issues: []model.ValidationIssue{{
				Code: "VALIDATION_FAILED", Severity: "error", Source: "test",
				Message: "check output password: hunter2", Remediation: "rotate token abc123TOKEN=",
			}},
		},
		Validations: []model.ConfigValidationResult{{
			Name: "sing-box check", Config: "cfg", Valid: false,
			Output: "stderr password: hunter2", Error: "exit 1 token abc123TOKEN=",
		}},
		ServiceActions: []model.ServiceActionResult{{
			Name: "restart sing-box", Success: false,
			Output: "log password: hunter2", Error: "Bearer abc123TOKEN=",
		}},
		HealthChecks: []model.ServiceHealthResult{{
			Name: "sing-box", Healthy: false, Output: "probe password: hunter2",
		}},
		RollbackActions: []model.ServiceActionResult{{
			Name: "rollback", Success: true, Output: "restored password: hunter2",
		}},
	}
	if err := state.applyHistoryLocked().Append("apply", false, response); err != nil {
		t.Fatalf("append history: %v", err)
	}

	wv := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apply/history", nil)
	state.handleApplyHistory(wv, viewerRoleRequest(req))
	if wv.Code != http.StatusOK {
		t.Fatalf("viewer history status=%d body=%s", wv.Code, wv.Body.String())
	}
	if strings.Contains(wv.Body.String(), "hunter2") || strings.Contains(wv.Body.String(), "abc123TOKEN") {
		t.Fatalf("viewer history leaked privileged output: %s", wv.Body.String())
	}

	wa := httptest.NewRecorder()
	state.handleApplyHistory(wa, adminRoleRequest(httptest.NewRequest(http.MethodGet, "/api/apply/history", nil)))
	if !strings.Contains(wa.Body.String(), "hunter2") {
		t.Fatalf("admin history lost raw diagnostics: %s", wa.Body.String())
	}
}

// #1065: GET /api/version/update/jobs/{id} is viewer-readable and its stored
// error message can embed privileged subprocess output from the apply runner.
func TestPanelUpdateJobRedactsErrorForViewers(t *testing.T) {
	state := &managementState{db: testdb.Open(t)}
	job, err := state.createPanelUpdateJob("v9.9.9")
	if err != nil {
		t.Fatalf("create update job: %v", err)
	}
	// Seed privileged-looking subprocess output as the stored job error.
	if _, err := state.db.Exec(`UPDATE panel_update_jobs SET status='failed', error_message=? WHERE id=?`,
		"apply failed password: hunter2", job.ID); err != nil {
		t.Fatal(err)
	}
	routes := PanelRoutes{State: state}

	wv := httptest.NewRecorder()
	routes.handlePanelUpdateJob(wv, viewerRoleRequest(httptest.NewRequest(http.MethodGet, "/api/version/update/jobs/"+job.ID, nil)))
	if wv.Code != http.StatusOK {
		t.Fatalf("viewer update job status=%d", wv.Code)
	}
	if strings.Contains(wv.Body.String(), "hunter2") {
		t.Fatalf("viewer update job leaked privileged output: %s", wv.Body.String())
	}

	wa := httptest.NewRecorder()
	routes.handlePanelUpdateJob(wa, adminRoleRequest(httptest.NewRequest(http.MethodGet, "/api/version/update/jobs/"+job.ID, nil)))
	if !strings.Contains(wa.Body.String(), "hunter2") {
		t.Fatalf("admin update job lost raw diagnostics: %s", wa.Body.String())
	}
}

// #1065: the shared SSE snapshot is broadcast to every subscriber — viewers
// included — so lastError is always sanitized at build time.
func TestSSESnapshotAlwaysRedactsLastError(t *testing.T) {
	state := newViewerSanitizationState(t)
	if err := state.applyJobs.Create(apply.Job{
		ID: "job-sse", DesiredRevision: 1, Status: apply.StatusFailed, Trigger: "manual", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := state.applyJobs.Finish("job-sse", apply.StatusFailed, "HEALTH_CHECK_FAILED",
		"health output password: hunter2"); err != nil {
		t.Fatalf("finish job: %v", err)
	}

	hub := &sseBroadcaster{state: state, subs: make(map[chan sseSnapshot]string)}
	snapshot := hub.buildSnapshot()
	if strings.Contains(string(snapshot.apply), "hunter2") {
		t.Fatalf("SSE snapshot leaked privileged output: %s", snapshot.apply)
	}
	if !strings.Contains(string(snapshot.apply), "[REDACTED]") {
		t.Fatalf("SSE snapshot not sanitized: %s", snapshot.apply)
	}
}
