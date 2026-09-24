package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every v1 mutation response must carry an HONEST envelope: success reflects
// the real apply outcome, revision.desired is the just-committed revision, and
// the applyJob (when present) is pinned to that revision.

func TestV1CreateResponseContainsRevisionAndSuccess(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"env-test"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("create success=false on a healthy apply: %s", w.Body.String())
	}
	if env.Revision.Desired < 1 {
		t.Errorf("create revision.desired=%d, want >=1", env.Revision.Desired)
	}
	if env.ApplyJob == nil {
		t.Fatalf("create response missing applyJob: %s", w.Body.String())
	}
	if env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("applyJob pinned to revision %d, want %d", env.ApplyJob.DesiredRevision, env.Revision.Desired)
	}
	if env.ApplyJob.ID == "" {
		t.Errorf("applyJob has no id")
	}
	if env.ApplyJob.Status == "" || env.ApplyJob.Status == "pending" || env.ApplyJob.Status == "queued" {
		t.Errorf("applyJob status=%q, want a terminal status", env.ApplyJob.Status)
	}
}

func TestV1UpdateResponseContainsRevisionAndSuccess(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"env-upd"}`)
	created := unwrapClient(t, w.Body.Bytes())
	id := created["id"].(string)

	w = v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, `{"version":1,"name":"env-upd-2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("update success=false on a healthy apply: %s", w.Body.String())
	}
	if env.Revision.Desired < 2 {
		t.Errorf("update revision.desired=%d, want >=2 (create+update)", env.Revision.Desired)
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("update applyJob mismatch: %+v", env.ApplyJob)
	}
}

func TestV1DeleteResponseContainsRevision(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"env-del"}`)
	created := unwrapClient(t, w.Body.Bytes())
	id := created["id"].(string)

	w = v1Request(t, r, http.MethodDelete, "/api/v1/clients/"+id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("delete success=false on a healthy apply: %s", w.Body.String())
	}
	if env.Revision.Desired < 2 {
		t.Errorf("delete revision.desired=%d, want >=2", env.Revision.Desired)
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("delete applyJob mismatch: %+v", env.ApplyJob)
	}
}

func TestV1BulkResponseShape(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"a"}`)
	a := unwrapClient(t, w.Body.Bytes())["id"].(string)
	w = v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"b"}`)
	b := unwrapClient(t, w.Body.Bytes())["id"].(string)

	w = v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk", `{"action":"disable","clientIds":["`+a+`","`+b+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	raw := w.Body.Bytes()
	var resp struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	}
	_ = json.Unmarshal(raw, &resp)
	if resp.Total != 2 || resp.Succeeded != 2 || resp.Failed != 0 {
		t.Fatalf("bulk counters wrong: %+v (body %s)", resp, w.Body.String())
	}
	env := decodeEnvelope(t, raw)
	if !env.Success {
		t.Errorf("bulk success=false when all clients applied: %s", w.Body.String())
	}
	if env.Revision.Desired < 3 {
		t.Errorf("bulk revision.desired=%d, want >=3", env.Revision.Desired)
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("bulk applyJob mismatch: %+v", env.ApplyJob)
	}
}

// TestV1BindingPatchAndDeleteEnvelopes proves the binding subresource returns
// the same honest mutation envelope as client mutations.
func TestV1BindingPatchAndDeleteEnvelopes(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	v1Request(t, r, http.MethodPost, "/api/inbounds", `{"name":"env-hy2","protocol":"hysteria2","transport":"udp","port":19543,"enabled":true}`)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"env-bind","bindings":[{"inboundId":"env-hy2","credential":"pw"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := unwrapClient(t, w.Body.Bytes())
	id := created["id"].(string)

	w = v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id+"/bindings", "")
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil || len(list.Items) != 1 {
		t.Fatalf("bindings list: %v %s", err, w.Body.String())
	}
	bindID := list.Items[0].ID

	// PATCH (disable) — mutation envelope must be honest.
	w = v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id+"/bindings/"+bindID, `{"enabled":false,"version":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("binding patch: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("binding patch success=false on a healthy apply: %s", w.Body.String())
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("binding patch applyJob mismatch: %+v", env.ApplyJob)
	}

	// DELETE — same contract.
	w = v1Request(t, r, http.MethodDelete, "/api/v1/clients/"+id+"/bindings/"+bindID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("binding delete: %d %s", w.Code, w.Body.String())
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("binding delete success=false on a healthy apply: %s", w.Body.String())
	}
}

// TestV1CredentialSetAndRotateEnvelopes proves credential writes return the
// honest mutation envelope (success + revision + applyJob) in addition to the
// one-time plaintext.
func TestV1CredentialSetAndRotateEnvelopes(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	v1Request(t, r, http.MethodPost, "/api/inbounds", `{"name":"env-cred","protocol":"hysteria2","transport":"udp","port":19544,"enabled":true}`)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"env-creds","bindings":[{"inboundId":"env-cred","credential":"pw-1"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := unwrapClient(t, w.Body.Bytes())
	id := created["id"].(string)

	w = v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id+"/bindings", "")
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil || len(list.Items) != 1 {
		t.Fatalf("bindings list: %v %s", err, w.Body.String())
	}
	bindID := list.Items[0].ID

	// Explicit credential set.
	w = v1Request(t, r, http.MethodPost, "/api/v1/clients/"+id+"/credentials/"+bindID, `{"kind":"password","value":"pw-2"}`)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("credential set: %d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("credential set success=false on a healthy apply: %s", w.Body.String())
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("credential set applyJob mismatch: %+v", env.ApplyJob)
	}

	// Server-side rotate returns plaintext exactly once plus the envelope.
	w = v1Request(t, r, http.MethodPost, "/api/v1/clients/"+id+"/credentials/"+bindID+"/rotate", `{"kind":"password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("credential rotate: %d %s", w.Code, w.Body.String())
	}
	var rot map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rot); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if pt, _ := rot["plaintext"].(string); pt == "" {
		t.Errorf("rotate response missing one-time plaintext: %v", rot)
	}
	env = decodeEnvelope(t, w.Body.Bytes())
	if !env.Success {
		t.Errorf("credential rotate success=false on a healthy apply: %s", w.Body.String())
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("credential rotate applyJob mismatch: %+v", env.ApplyJob)
	}
}

// createV1Client posts a minimal client create and returns the new client's
// id — shared by sibling v1 tests that only need a client to exist.
func createV1Client(t *testing.T, r http.Handler, name string) string {
	t.Helper()
	body := strings.NewReader(`{"name":"` + name + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	// The created object may be top-level or under "client"/"id".
	if c, ok := resp["client"].(map[string]any); ok {
		if id, ok := c["id"].(string); ok {
			return id
		}
	}
	if id, ok := resp["id"].(string); ok {
		return id
	}
	t.Fatalf("no client id in response: %v", resp)
	return ""
}

// keysOf returns the sorted key list of a JSON object — used by sibling v1
// tests to report unexpected response shapes.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
