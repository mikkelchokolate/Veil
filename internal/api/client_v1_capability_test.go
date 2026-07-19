package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1ClientViewIncludesBindingCapabilities asserts (A7) that GET a client
// returns a bindings read model where each binding carries its inbound's
// protocol capabilities (protocol, transports, per-client credential support),
// not just a bare list of inbound IDs.
func TestV1ClientViewIncludesBindingCapabilities(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// Inbound to bind to.
	inboundBody := strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK && iw.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", iw.Code, iw.Body.String())
	}

	// Client with a binding to it.
	id := createV1ClientWithBinding(t, r, "cap-client", "hy2", "pass-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get client: %d %s", w.Code, w.Body.String())
	}
	var view map[string]any
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	bindings, ok := view["bindings"].([]any)
	if !ok || len(bindings) == 0 {
		t.Fatalf("expected bindings array in client view, got: %v", keysOf(view))
	}
	b0, _ := bindings[0].(map[string]any)
	if b0["inboundId"] != "hy2" {
		t.Errorf("binding inboundId=%v, want hy2", b0["inboundId"])
	}
	cap, ok := b0["capability"].(map[string]any)
	if !ok {
		t.Fatalf("binding missing capability object: %v", b0)
	}
	if cap["protocol"] != "hysteria2" {
		t.Errorf("capability protocol=%v, want hysteria2", cap["protocol"])
	}
	if _, ok := cap["transports"]; !ok {
		t.Errorf("capability missing transports: %v", cap)
	}
	if _, ok := cap["perClientCredentials"]; !ok {
		t.Errorf("capability missing perClientCredentials: %v", cap)
	}
}

func createV1ClientWithBinding(t *testing.T, r http.Handler, name, inboundID, cred string) string {
	t.Helper()
	body := strings.NewReader(`{"name":"` + name + `","bindings":[{"inboundId":"` + inboundID + `","credential":"` + cred + `"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, ok := resp["id"].(string); ok {
		return id
	}
	t.Fatalf("no id: %v", resp)
	return ""
}
