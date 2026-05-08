package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestInboundCreateThroughHTTPGeneratesClientProfilePassword(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	body := strings.NewReader(`{"name":"naive-profiles","protocol":"naiveproxy","transport":"tcp","port":9443,"enabled":true,"profiles":[{"name":"alice","username":"alice","enabled":true}]}`)
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
	if len(inbound.Profiles) != 1 || inbound.Profiles[0].Password == "" {
		t.Fatalf("expected generated client profile password: %+v", inbound)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "naive+https://alice:"+inbound.Profiles[0].Password+"@vpn.example.com:9443") {
		t.Fatalf("client links do not use generated client profile password: %s", w2.Body.String())
	}
}
