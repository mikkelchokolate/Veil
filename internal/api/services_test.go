package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestServicesRestartRejectsGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/services/veil/restart", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestServicesRestartRejectsInvalidService(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/evil/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid service, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRestartRequiresConfirm(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/veil/restart", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without confirm, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServicesRestartSuccess(t *testing.T) {
	var gotCommand []string
	orig := serviceActionRunner
	serviceActionRunner = func(command []string) ServiceActionResult {
		gotCommand = append([]string(nil), command...)
		return ServiceActionResult{Name: command[2], Command: command, Success: true}
	}
	defer func() { serviceActionRunner = orig }()

	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/caddy/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Lock the response envelope, not just the status — the stubbed runner
	// must have driven a restart of the real caddy unit.
	if want := []string{"systemctl", "restart", "veil-caddy.service"}; !reflect.DeepEqual(gotCommand, want) {
		t.Fatalf("runner command = %v, want %v", gotCommand, want)
	}
	var resp struct {
		Service string `json:"service"`
		Action  string `json:"action"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, w.Body.String())
	}
	if resp.Service != "caddy" || resp.Action != "restart" || !resp.Success {
		t.Fatalf("unexpected response envelope: %+v", resp)
	}
}

func TestServicesRestartFailure(t *testing.T) {
	orig := serviceActionRunner
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Name: command[2], Command: command, Success: false, Error: "failed"}
	}
	defer func() { serviceActionRunner = orig }()

	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/hysteria2/restart", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	// The envelope must carry the structured error code; the runner's raw
	// stderr is intentionally redacted behind the generic message.
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (body %s)", err, w.Body.String())
	}
	if body.Error.Code != "operation_failed" {
		t.Fatalf("error code = %q, want operation_failed", body.Error.Code)
	}
	if body.Error.Message != "privileged operation failed" {
		t.Fatalf("error message = %q, want the redacted public message", body.Error.Message)
	}
}

func TestServicesRejectsUnsupportedAction(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/services/veil/stop", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported action, got %d", w.Code)
	}
}
