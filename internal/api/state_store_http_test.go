package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInboundPasswordSavedThroughHTTPIsEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, KeyPath: keyPath})

	body := strings.NewReader(`{"name":"hy2-secret","protocol":"hysteria2","transport":"udp","port":9555,"enabled":true,"password":"plain-inbound-secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.Contains(string(raw), "plain-inbound-secret") {
		t.Fatalf("state leaked inbound password: %s", string(raw))
	}
}
