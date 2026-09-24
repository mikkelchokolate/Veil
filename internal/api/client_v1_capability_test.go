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
	// A capability whose transports list is absent or empty is not a usable
	// capability — the panel needs the concrete transport list.
	transports, _ := cap["transports"].([]any)
	if len(transports) == 0 {
		t.Errorf("capability transports empty: %v", cap)
	} else {
		found := false
		for _, tr := range transports {
			if tr == "udp" {
				found = true
			}
		}
		if !found {
			t.Errorf("capability transports %v missing the bound udp transport", transports)
		}
	}
	if cap["perClientCredentials"] != true {
		t.Errorf("hysteria2 perClientCredentials=%v, want true", cap["perClientCredentials"])
	}
}

// TestV1ClientViewOlcrtcDoesNotAdvertisePerClientEnforcement covers audit
// #309: olcRTC renders per-client links, but every client shares the
// inbound-wide encryption key, so the binding capability must not claim
// per-client credential rotation or expiry enforcement.
func TestV1ClientViewOlcrtcDoesNotAdvertisePerClientEnforcement(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	inboundBody := strings.NewReader(`{"name":"rtc","protocol":"olcrtc","transport":"udp","port":8443,"enabled":true}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK && iw.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", iw.Code, iw.Body.String())
	}

	id := createV1ClientWithBinding(t, r, "rtc-client", "rtc", "pass-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get client: %d %s", w.Code, w.Body.String())
	}
	var view map[string]any
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	bindings, _ := view["bindings"].([]any)
	if len(bindings) == 0 {
		t.Fatalf("expected bindings array in client view, got: %v", keysOf(view))
	}
	b0, _ := bindings[0].(map[string]any)
	cap, ok := b0["capability"].(map[string]any)
	if !ok {
		t.Fatalf("binding missing capability object: %v", b0)
	}
	if cap["protocol"] != "olcrtc" {
		t.Errorf("capability protocol=%v, want olcrtc", cap["protocol"])
	}
	if cap["perClientCredentials"] != false {
		t.Errorf("olcrtc perClientCredentials=%v, want false (shared inbound key)", cap["perClientCredentials"])
	}
	if cap["expirationEnforcement"] != false {
		t.Errorf("olcrtc expirationEnforcement=%v, want false (shared inbound key)", cap["expirationEnforcement"])
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
	// S2 nested the created client under "client"; tolerate both shapes.
	if c, ok := resp["client"].(map[string]any); ok {
		resp = c
	}
	if id, ok := resp["id"].(string); ok {
		return id
	}
	t.Fatalf("no id: %v", resp)
	return ""
}
