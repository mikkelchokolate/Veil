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
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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
