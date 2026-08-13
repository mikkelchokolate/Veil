package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

func TestInboundCreateThroughHTTPGeneratesPasswordWhenMissing(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	body := strings.NewReader(`{"name":"hy2-auto","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var inbound Inbound
	if err := json.NewDecoder(w.Body).Decode(&inbound); err != nil {
		t.Fatalf("decode inbound: %v", err)
	}
	if inbound.Password != veilsettings.RedactedSecret {
		t.Fatalf("expected redacted password in response, got %q", inbound.Password)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	// The redacted response must not leak the generated password; client links
	// should still contain the real value for the created inbound.
	if !strings.Contains(w2.Body.String(), "hysteria2://") || !strings.Contains(w2.Body.String(), "@vpn.example.com:9443/") {
		t.Fatalf("client links missing generated inbound: %s", w2.Body.String())
	}
}

// TestInboundPutWithRedactedEchoPreservesLiveCredentials is the regression for
// the panel round-trip: GET returns "[REDACTED]" for password fields, the SPA
// echoes them back verbatim on PUT, and saving the echo must not replace the
// live credential with the sentinel (which would break every client binding).
func TestInboundPutWithRedactedEchoPreservesLiveCredentials(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	createBody := `{"name":"hy2-edit","protocol":"hysteria2","transport":"udp","port":9444,"enabled":true,"protocolFields":{"hysteria2Password":"panel-supplied-pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created Inbound
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode inbound: %v", err)
	}
	if created.Password != veilsettings.RedactedSecret {
		t.Fatalf("expected redacted password in response, got %q", created.Password)
	}

	// The panel echoes the redacted representation back on save.
	echoBody := `{"name":"hy2-edit","protocol":"hysteria2","transport":"udp","port":9445,"enabled":true,"password":"[REDACTED]","protocolFields":{"hysteria2Password":"[REDACTED]"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/inbounds/hy2-edit", strings.NewReader(echoBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// The stored credential must remain the panel-supplied value, and client
	// links must still resolve with it.
	req3 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	links := w3.Body.String()
	if strings.Contains(links, "[REDACTED]") {
		t.Fatalf("client links leak the redaction sentinel: %s", links)
	}
	if !strings.Contains(links, "panel-supplied-pw") {
		t.Fatalf("client links lost the live credential after redacted echo PUT: %s", links)
	}
	if !strings.Contains(links, ":9445/") {
		t.Fatalf("client links did not reflect the port update: %s", links)
	}
}

// TestMieruPutPromotesNewProtocolPassword is the regression for the update
// path: a new password submitted through the panel's dynamic field
// (protocolFields["password"]) must reach the canonical flat Password field the
// mieru renderer consumes, even though Autofill historically ran on POST only.
func TestMieruPutPromotesNewProtocolPassword(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	createBody := `{"name":"mieru-edit","protocol":"mieru","transport":"tcp","port":9446,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The panel changes the password via the dynamic field on save.
	updateBody := `{"name":"mieru-edit","protocol":"mieru","transport":"tcp","port":9446,"enabled":true,"protocolFields":{"password":"rotated-pw"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/inbounds/mieru-edit", strings.NewReader(updateBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	links := w3.Body.String()
	if !strings.Contains(links, "rotated-pw") {
		t.Fatalf("mieru client config lost the rotated password after PUT: %s", links)
	}
	if strings.Contains(links, "[REDACTED]") {
		t.Fatalf("mieru client config leaks the redaction sentinel: %s", links)
	}
}
