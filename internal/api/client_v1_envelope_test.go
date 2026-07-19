package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1ClientCreateReturnsMutationEnvelope asserts (A4) that creating a
// client returns the honest mutation envelope: the created object plus the
// desired/applied revision and the apply job outcome — not a bare object with
// the apply result silently discarded via a void notifier.
func TestV1ClientCreateReturnsMutationEnvelope(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	body := strings.NewReader(`{"name":"env-client","email":"e@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", body)
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
		t.Errorf("create response missing revision: %v", keysOf(resp))
	}
	if _, ok := resp["success"]; !ok {
		t.Errorf("create response missing success: %v", keysOf(resp))
	}
}

// TestV1ClientUpdateReturnsMutationEnvelope covers PUT update.
func TestV1ClientUpdateReturnsMutationEnvelope(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	id := createV1Client(t, r, "upd-client")

	body := strings.NewReader(`{"version":1,"name":"upd-client-renamed"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/clients/"+id, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["revision"]; !ok {
		t.Errorf("update response missing revision: %v", keysOf(resp))
	}
	if _, ok := resp["success"]; !ok {
		t.Errorf("update response missing success: %v", keysOf(resp))
	}
}

// TestV1ClientDeleteReturnsMutationEnvelope covers DELETE.
func TestV1ClientDeleteReturnsMutationEnvelope(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	id := createV1Client(t, r, "del-client")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/clients/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["revision"]; !ok {
		t.Errorf("delete response missing revision: %v", keysOf(resp))
	}
}

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

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
