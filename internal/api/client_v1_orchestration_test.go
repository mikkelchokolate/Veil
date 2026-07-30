package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// Strict client-mutation/apply orchestration tests. Unlike the envelope tests
// (which assert presence of fields) these assert CORRECTNESS:
//  1. desired revision increases by exactly one per mutation;
//  2. exactly one apply job is created per mutation;
//  3. the job points at the new revision;
//  4. the immutable snapshot for that revision contains the client;
//  5. the response carries that exact job;
//  6. an apply failure surfaces success:false;
//  7. update/delete/binding/credential mutations never create two jobs.

type mutationEnvelope struct {
	Revision struct {
		Desired uint64 `json:"desired"`
		Applied uint64 `json:"applied"`
		State   string `json:"state"`
	} `json:"revision"`
	ApplyJob *struct {
		ID              string `json:"id"`
		DesiredRevision uint64 `json:"desiredRevision"`
		Status          string `json:"status"`
	} `json:"applyJob"`
	Success bool `json:"success"`
}

func decodeEnvelope(t *testing.T, body []byte) mutationEnvelope {
	t.Helper()
	var env mutationEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (body %q)", err, body)
	}
	return env
}

func applyState(t *testing.T, r http.Handler) (desired, applied uint64) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/state", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("apply state: %d %s", w.Code, w.Body.String())
	}
	var st struct {
		DesiredRevision uint64 `json:"desiredRevision"`
		AppliedRevision uint64 `json:"appliedRevision"`
	}
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode apply state: %v", err)
	}
	return st.DesiredRevision, st.AppliedRevision
}

func listApplyJobs(t *testing.T, r http.Handler) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/jobs?limit=100", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("apply jobs: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	return resp.Items
}

func postJSON(t *testing.T, r http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestClientMutationOrchestration verifies the full lifecycle: create then
// update then delete, each bumping desired by exactly one and producing
// exactly one job pinned to the new revision, with the response carrying
// that exact job.
func TestClientMutationOrchestration(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	desired0, _ := applyState(t, r)
	jobs0 := len(listApplyJobs(t, r))

	// --- CREATE ---
	w := postJSON(t, r, "/api/v1/clients", `{"name":"orch-client"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())

	// (1) desired revision increased by exactly one.
	if env.Revision.Desired != desired0+1 {
		t.Errorf("(1) desired revision = %d, want %d", env.Revision.Desired, desired0+1)
	}
	// (2) exactly one new apply job.
	jobs := listApplyJobs(t, r)
	if len(jobs) != jobs0+1 {
		t.Fatalf("(2) jobs after create = %d, want %d", len(jobs), jobs0+1)
	}
	// (3) the job points at the new revision.
	if env.ApplyJob == nil {
		t.Fatalf("(5) create response missing applyJob")
	}
	if env.ApplyJob.DesiredRevision != desired0+1 {
		t.Errorf("(3) job desiredRevision = %d, want %d", env.ApplyJob.DesiredRevision, desired0+1)
	}
	// (5) response job is the exact job in the store (newest).
	newest := jobs[0]
	if newest["id"] != env.ApplyJob.ID {
		t.Errorf("(5) response job %v is not the store's newest job %v", env.ApplyJob.ID, newest["id"])
	}
	// job must be terminal (runner is synchronous): never "queued/pending".
	if st := env.ApplyJob.Status; st == "pending" || st == "applying" || st == "queued" {
		t.Errorf("(5) job returned non-terminal status %q", st)
	}
	if !env.Success {
		t.Errorf("create success = false with healthy apply: %+v", env)
	}
	// (5) response carries the exact client id.
	clientID := func() string {
		var raw map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		if c, ok := raw["client"].(map[string]any); ok {
			if id, ok := c["id"].(string); ok {
				return id
			}
		}
		id, _ := raw["id"].(string)
		return id
	}()
	if clientID == "" {
		t.Fatalf("(5) create response has no client id: %s", w.Body.String())
	}

	// --- UPDATE (revision+job, not two jobs) ---
	putReq := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/"+clientID, strings.NewReader(`{"version":1,"name":"orch-renamed"}`))
	putReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, putReq)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if env.Revision.Desired != desired0+2 {
		t.Errorf("(1) update desired = %d, want %d", env.Revision.Desired, desired0+2)
	}
	// (7) exactly one additional job.
	if got := len(listApplyJobs(t, r)); got != jobs0+2 {
		t.Errorf("(7) jobs after update = %d, want %d (double apply!)", got, jobs0+2)
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != desired0+2 {
		t.Errorf("(3) update job mismatch: %+v", env.ApplyJob)
	}

	// --- BINDING mutation: exactly one job ---
	inbound := "orch-inbound"
	w = postJSON(t, r, "/api/inbounds", fmt.Sprintf(`{"name":%q,"protocol":"hysteria2","transport":"udp","port":14430,"enabled":true}`, inbound))
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	desiredBeforeBind, _ := applyState(t, r)
	jobsBeforeBind := len(listApplyJobs(t, r))
	w = postJSON(t, r, "/api/v1/clients/"+clientID+"/bindings", fmt.Sprintf(`{"inboundId":%q}`, inbound))
	if w.Code != http.StatusCreated {
		t.Fatalf("add binding: %d %s", w.Code, w.Body.String())
	}
	if got := len(listApplyJobs(t, r)); got != jobsBeforeBind+1 {
		t.Errorf("(7) binding add created %d jobs, want 1", got-jobsBeforeBind)
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if env.Revision.Desired != desiredBeforeBind+1 {
		t.Errorf("(1) binding desired = %d, want %d", env.Revision.Desired, desiredBeforeBind+1)
	}
	var bindResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &bindResp)

	// --- CREDENTIAL rotation: exactly one job, plaintext returned once ---
	desiredBeforeCred, _ := applyState(t, r)
	jobsBeforeCred := len(listApplyJobs(t, r))
	w = postJSON(t, r, "/api/v1/clients/"+clientID+"/credentials/"+bindResp.ID+"/rotate", `{"kind":"password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}
	if got := len(listApplyJobs(t, r)); got != jobsBeforeCred+1 {
		t.Errorf("(7) credential rotate created %d jobs, want 1", got-jobsBeforeCred)
	}
	var rotResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &rotResp)
	if pt, _ := rotResp["plaintext"].(string); pt == "" {
		t.Errorf("(rotate) server-generated plaintext missing from response")
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if env.Revision.Desired != desiredBeforeCred+1 {
		t.Errorf("(1) rotate desired = %d, want %d", env.Revision.Desired, desiredBeforeCred+1)
	}

	// --- DELETE: exactly one job ---
	desiredBeforeDel, _ := applyState(t, r)
	jobsBeforeDel := len(listApplyJobs(t, r))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/clients/"+clientID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if got := len(listApplyJobs(t, r)); got != jobsBeforeDel+1 {
		t.Errorf("(7) delete created %d jobs, want 1", got-jobsBeforeDel)
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if env.Revision.Desired != desiredBeforeDel+1 {
		t.Errorf("(1) delete desired = %d, want %d", env.Revision.Desired, desiredBeforeDel+1)
	}
}

// TestClientMutationSnapshotContainsClient (4) verifies the immutable snapshot
// recorded for the create revision contains the created client.
func TestClientMutationSnapshotContainsClient(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	// An enabled inbound is required for the plan to render a config that
	// embeds the bound client (no inbounds -> empty plan -> nothing to find).
	w := postJSON(t, r, "/api/inbounds", `{"name":"snap-inbound","protocol":"hysteria2","transport":"udp","port":14431,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	w = postJSON(t, r, "/api/v1/clients", `{"name":"snap-client","bindings":[{"inboundId":"snap-inbound"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	_ = decodeEnvelope(t, w.Body.Bytes())
	// (4) The immutable snapshot recorded for the create revision must contain
	// the created client. Read the snapshot store directly (the same store the
	// apply executor pins from) and assert the client name is present.
	env := decodeEnvelope(t, w.Body.Bytes())
	if st.applySnapshots == nil {
		t.Fatalf("(4) snapshot store unavailable")
	}
	payload, err := st.applySnapshots.Load(env.Revision.Desired)
	if err != nil {
		t.Fatalf("(4) load snapshot for revision %d: %v", env.Revision.Desired, err)
	}
	if !strings.Contains(string(payload), "snap-client") {
		t.Errorf("(4) revision %d snapshot does not contain created client: %s",
			env.Revision.Desired, payload)
	}
}

// stateOf is unused placeholder retained for API symmetry; see
// newApplyTrackedRouterWithState below.

// TestClientCreateIssuesGeneratedCredentials (S2) verifies that creating a
// client with bindings and no explicit credential returns the generated
// plaintext exactly once in issuedCredentials, that the plaintext is NOT the
// stored (encrypted) form, and that the credential actually works (is in the
// snapshot so the apply renders it).
func TestClientCreateIssuesGeneratedCredentials(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	w := postJSON(t, r, "/api/inbounds", `{"name":"iss-inbound","protocol":"hysteria2","transport":"udp","port":14433,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	w = postJSON(t, r, "/api/v1/clients", `{"name":"iss-client","bindings":[{"inboundId":"iss-inbound"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Client struct {
			ID string `json:"id"`
		} `json:"client"`
		Issued []struct {
			BindingID string `json:"bindingId"`
			InboundID string `json:"inboundId"`
			Kind      string `json:"kind"`
			Plaintext string `json:"plaintext"`
		} `json:"issuedCredentials"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if len(resp.Issued) != 1 {
		t.Fatalf("issuedCredentials len = %d, want 1: %s", len(resp.Issued), w.Body.String())
	}
	iss := resp.Issued[0]
	if iss.Plaintext == "" {
		t.Errorf("issued credential has empty plaintext")
	}
	if iss.InboundID != "iss-inbound" {
		t.Errorf("issued inboundId = %q, want iss-inbound", iss.InboundID)
	}
	if iss.BindingID == "" {
		t.Errorf("issued bindingId empty")
	}
	// The stored credential must be the encrypted form, not the plaintext, and
	// must decrypt back to the issued plaintext (proves encrypted at rest and
	// retrievable). Use the credential store directly.
	if st.clientCreds == nil {
		t.Fatalf("credential store unavailable")
	}
	cred, err := st.clientCreds.ActiveForBinding(iss.BindingID, "password")
	if err != nil {
		t.Fatalf("active credential: %v", err)
	}
	if len(cred.EncryptedValue) == 0 {
		t.Errorf("credential has no encrypted value (stored as plaintext?)")
	}
	if string(cred.EncryptedValue) == iss.Plaintext {
		t.Errorf("credential stored as plaintext, must be encrypted")
	}
	revealed, err := st.clientCreds.Reveal(cred.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed != iss.Plaintext {
		t.Errorf("revealed plaintext does not match issued plaintext")
	}
	// The immutable snapshot for the client's revision must contain it. The
	// client create is the second mutation (inbound create was revision 1).
	env := decodeEnvelope(t, w.Body.Bytes())
	payload, err := st.applySnapshots.Load(env.Revision.Desired)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !strings.Contains(string(payload), "iss-client") {
		t.Errorf("snapshot missing created client")
	}
}

// newApplyTrackedRouterWithState mirrors newApplyTrackedRouter but also
// returns the managementState so tests can inspect the snapshot store
// directly (the same store the apply executor pins from).
func newApplyTrackedRouterWithState(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origHealth := serviceHealthChecker
	origAutoApply := autoApplyAfterMutation
	origFirewall := firewallApplierInstance
	t.Cleanup(func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		serviceHealthChecker = origHealth
		autoApplyAfterMutation = origAutoApply
		firewallApplierInstance = origFirewall
	})
	// See newApplyTrackedRouter: unit tests never touch the host firewall.
	firewallApplierInstance = &fakeFirewallApplier{}
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: true})
		}
		return out
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	autoApplyAfterMutation = true

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, reloader := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: dir})
	st, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close apply-tracked management state: %v", err)
		}
	})
	return r, st
}

// TestClientMutationApplyFailureSuccessFalse (6) forces an apply failure and
// asserts the mutation response reports success:false with the failed job.
func TestClientMutationApplyFailureSuccessFalse(t *testing.T) {
	// Make the apply FAIL by binding the new client to an enabled inbound: that
	// promotes a rendered config, triggering a service reload we force to fail.
	// The workflow rolls back -> terminal failed job -> success:false.
	r, _ := newApplyTrackedRouter(t)
	w := postJSON(t, r, "/api/inbounds", `{"name":"fail-inbound","protocol":"hysteria2","transport":"udp","port":14432,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: false, Error: "forced reload failure"}
	}
	w = postJSON(t, r, "/api/v1/clients", `{"name":"fail-client","bindings":[{"inboundId":"fail-inbound"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if env.Success {
		t.Errorf("(6) success = true despite failed apply: %+v", env)
	}
	// A rolled-back apply is the terminal failure state for a health/reload
	// failure; both "failed" and "rolled_back" are non-success terminals.
	if env.ApplyJob == nil || (env.ApplyJob.Status != "failed" && env.ApplyJob.Status != "rolled_back") {
		t.Errorf("(6) applyJob status = %+v, want failed/rolled_back", env.ApplyJob)
	}
}

// TestV1ClientAudit verifies the per-client audit endpoint returns only the
// entries scoped to that client (target = client ID or name), newest first.
func TestV1ClientAudit(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	w := postJSON(t, r, "/api/v1/clients", `{"name":"audit-client"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	clientID := func() string {
		var raw map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		if c, ok := raw["client"].(map[string]any); ok {
			if id, ok := c["id"].(string); ok {
				return id
			}
		}
		id, _ := raw["id"].(string)
		return id
	}()
	if clientID == "" {
		t.Fatalf("no client id in create response: %s", w.Body.String())
	}
	// Trigger a client-scoped mutation so there is at least one entry with
	// target == clientID.
	putReq := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/"+clientID, strings.NewReader(`{"name":"audit-client","version":1}`))
	putReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, putReq)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+clientID+"/audit", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", w2.Code, w2.Body.String())
	}
	var body struct {
		Items []struct {
			Action string `json:"action"`
			Target string `json:"target"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(body.Items) == 0 {
		t.Fatalf("expected at least one audit entry for client, got none")
	}
	for _, it := range body.Items {
		if it.Target != clientID && it.Target != "audit-client" {
			t.Errorf("audit entry leaked another client's target: %+v", it)
		}
	}
}

// TestTrafficProvidersRefreshOnApply (S6) asserts that traffic providers are
// re-registered after a mutation-driven apply so attribution tracks inbound
// changes without a restart. Before any inbound exists there are no
// providers; adding an enabled hysteria2 inbound + applying must register one.
func TestTrafficProvidersRefreshOnApply(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	if st.trafficCollector == nil {
		t.Skip("traffic collector not initialized")
	}
	before := st.trafficCollector.ProviderCount()
	w := postJSON(t, r, "/api/inbounds", `{"name":"s6-in","protocol":"hysteria2","transport":"udp","port":14440,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	// The inbound create triggers a mutation apply, which refreshes providers.
	after := st.trafficCollector.ProviderCount()
	if after <= before {
		t.Errorf("(S6) provider count did not increase after apply: before=%d after=%d", before, after)
	}
}
