package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementAPICreatesMieruTCPAndUDPInbounds(t *testing.T) {
	stubManagementApplySideEffects(t)
	r, reloader := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	if state, ok := reloader.(*managementState); ok {
		t.Cleanup(func() {
			if err := state.Close(); err != nil {
				t.Errorf("close management state: %v", err)
			}
		})
	}
	for _, body := range []string{
		`{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"profiles":[{"name":"test","password":"test-password","enabled":true}]}`,
		`{"name":"mieru-udp","protocol":"mieru","transport":"udp","port":443,"enabled":true,"password":"test-password"}`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(body)))
		if w.Code != http.StatusCreated {
			t.Fatalf("create Mieru inbound expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestManagementAPIRejectsUnsupportedInboundProtocolAndTransport(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	for _, body := range []string{
		`{"name":"unknown","protocol":"unknown","transport":"tcp","port":443,"enabled":true}`,
		`{"name":"hy2-tcp","protocol":"hysteria2","transport":"tcp","port":8443,"enabled":true}`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "unsupported inbound protocol/transport") {
			t.Fatalf("expected protocol/transport error, got: %s", w.Body.String())
		}
	}
}
