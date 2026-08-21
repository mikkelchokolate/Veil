package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// newSettingsEchoRouter builds a router with the apply subsystem stubbed to
// succeed and returns the live management state so tests can assert on the
// committed settings (the API GET always redacts secrets by design).
func newSettingsEchoRouter(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origHealth := serviceHealthChecker
	origAutoApply := autoApplyAfterMutation
	origFirewall := currentFirewallApplier()
	t.Cleanup(func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		serviceHealthChecker = origHealth
		autoApplyAfterMutation = origAutoApply
		swapFirewallApplier(origFirewall)
	})
	swapFirewallApplier(&fakeFirewallApplier{})
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: true})
		}
		return out
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	autoApplyAfterMutation = true

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, reloader := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: dir})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	return r, state
}

func putSettings(t *testing.T, r http.Handler, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
	return w.Code
}

// TestSettingsEchoPutFullObjectPreservesSecrets is the regression for the
// Panel SPA save flow: the SPA GETs redacted settings, overlays the edited
// field, and PUTs the full object back (it must include panelListen/mode).
// The live secrets survive the redacted echo and the edit is applied.
func TestSettingsEchoPutFullObjectPreservesSecrets(t *testing.T) {
	r, state := newSettingsEchoRouter(t)

	if code := putSettings(t, r, `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","naivePassword":"naive-live","hysteria2Password":"hy-live"}`); code != http.StatusOK {
		t.Fatalf("seed put: %d", code)
	}

	// SPA round trip: GET (redacted) -> overlay edit -> PUT full object.
	wg := httptest.NewRecorder()
	r.ServeHTTP(wg, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("get: %d", wg.Code)
	}
	echo := map[string]any{}
	if err := json.Unmarshal(wg.Body.Bytes(), &echo); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if echo["naivePassword"] != "[REDACTED]" || echo["hysteria2Password"] != "[REDACTED]" {
		t.Fatalf("GET must redact secrets: %+v", echo)
	}
	echo["domain"] = "new.example.com"
	echoBody, _ := json.Marshal(echo)

	if code := putSettings(t, r, string(echoBody)); code != http.StatusOK {
		t.Fatalf("echo put: %d", code)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.Domain != "new.example.com" {
		t.Fatalf("domain edit lost: %+v", state.settings)
	}
	if state.settings.NaivePassword != "naive-live" || state.settings.Hysteria2Password != "hy-live" {
		t.Fatalf("live secrets were not preserved through redacted echo: %+v", state.settings)
	}
	if state.settings.ProtocolFields["naivePassword"] != "naive-live" {
		t.Fatalf("protocol secret field not preserved: %+v", state.settings.ProtocolFields)
	}
}

// TestSettingsEchoPutRotationWinsOverEchoedSentinel ensures a genuinely new
// password submitted by the SPA overrides the redaction sentinel.
func TestSettingsEchoPutRotationWinsOverEchoedSentinel(t *testing.T) {
	r, state := newSettingsEchoRouter(t)

	if code := putSettings(t, r, `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","naivePassword":"old-secret"}`); code != http.StatusOK {
		t.Fatalf("seed put: %d", code)
	}
	if code := putSettings(t, r, `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","naivePassword":"fresh-secret"}`); code != http.StatusOK {
		t.Fatalf("rotate put: %d", code)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.NaivePassword != "fresh-secret" {
		t.Fatalf("rotation did not win: %+v", state.settings)
	}
}

// TestSettingsPartialPutWithoutRequiredFieldsFailsValidation documents the
// API contract: a partial payload missing panelListen/mode is rejected (the
// SPA therefore must send the full object, which is what the echo tests
// exercise).
func TestSettingsPartialPutWithoutRequiredFieldsFailsValidation(t *testing.T) {
	r, _ := newSettingsEchoRouter(t)
	code := putSettings(t, r, `{"domain":"only-this.example.com"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("partial put: got %d, want 400", code)
	}
}
