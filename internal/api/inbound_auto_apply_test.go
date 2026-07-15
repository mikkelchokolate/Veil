package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func TestInboundCreateTriggersAutoApply(t *testing.T) {
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origAutoApply := autoApplyAfterInboundMutation
	defer func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		autoApplyAfterInboundMutation = origAutoApply
	}()

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: path, Config: path, Valid: true})
		}
		return results
	}

	var calls [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		calls = append(calls, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	body := strings.NewReader(`{"name":"hy2-auto","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Auto-apply must have triggered at least a service action for the hysteria2 unit.
	found := false
	for _, c := range calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[2] == "veil-hysteria2@hy2-auto.service" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-apply to restart veil-hysteria2@hy2-auto.service, calls=%v", calls)
	}
}

func TestInboundUpdateTriggersAutoApply(t *testing.T) {
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origAutoApply := autoApplyAfterInboundMutation
	defer func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		autoApplyAfterInboundMutation = origAutoApply
	}()

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: path, Config: path, Valid: true})
		}
		return results
	}

	var calls [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		calls = append(calls, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	// Create seed inbound first (auto-apply runs here too).
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-update","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)))
	calls = nil

	// Update the inbound and verify auto-apply runs again.
	update := httptest.NewRequest(http.MethodPut, "/api/inbounds/hy2-update", strings.NewReader(`{"protocol":"hysteria2","transport":"udp","port":9444,"enabled":true}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	found := false
	for _, c := range calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[2] == "veil-hysteria2@hy2-update.service" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-apply to restart veil-hysteria2@hy2-update.service after update, calls=%v", calls)
	}
}

func TestInboundDeleteTriggersAutoApply(t *testing.T) {
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origAutoApply := autoApplyAfterInboundMutation
	defer func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		autoApplyAfterInboundMutation = origAutoApply
	}()

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: path, Config: path, Valid: true})
		}
		return results
	}

	var calls [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		calls = append(calls, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-delete","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)))

	// Seed a live config so deletion has an orphan to tear down.
	liveConfig := filepath.Join(applyRoot, "live", "hysteria2", "hy2-delete.yaml")
	if err := atomicfile.Write(liveConfig, []byte("listen: :9443\n"), 0o600, 0o700); err != nil {
		t.Fatalf("write live config: %v", err)
	}
	calls = nil

	del := httptest.NewRequest(http.MethodDelete, "/api/inbounds/hy2-delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, del)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// After delete the runtime should be stopped/disabled as an orphan.
	found := false
	for _, c := range calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[2] == "veil-hysteria2@hy2-delete.service" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-apply to act on veil-hysteria2@hy2-delete.service after delete, calls=%v", calls)
	}
}
