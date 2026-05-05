package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServicesRestartRejectsGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/services/veil/restart", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestServicesRestartRejectsInvalidService(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/evil/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid service, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRestartRequiresConfirm(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/veil/restart", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without confirm, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRestartSuccess(t *testing.T) {
	orig := serviceControlRunner
	serviceControlRunner = func(name, action string) ServiceActionResponse {
		return ServiceActionResponse{Service: name, Action: action, Success: true, Output: "restarted"}
	}
	defer func() { serviceControlRunner = orig }()

	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/caddy/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRestartFailure(t *testing.T) {
	orig := serviceControlRunner
	serviceControlRunner = func(name, action string) ServiceActionResponse {
		return ServiceActionResponse{Service: name, Action: action, Success: false, Error: "failed"}
	}
	defer func() { serviceControlRunner = orig }()

	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/hysteria2/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRejectsUnsupportedAction(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/veil/stop", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported action, got %d", w.Code)
	}
}
