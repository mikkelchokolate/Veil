package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func TestSettingsPutWebBasePathDoesNotRetargetPanelMount(t *testing.T) {
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
	seed := `{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/oldsecret/","mode":"dev","domain":"panel.example.com","email":"admin@example.com"}}`
	if err := atomicfile.Write(statePath, []byte(seed), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		StatePath:   statePath,
		ApplyRoot:   dir,
		PanelListen: "127.0.0.1:2096",
		PanelAccess: "caddy",
		WebBasePath: "/oldsecret/",
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })

	oldReq := httptest.NewRequest(http.MethodGet, "/oldsecret/api/settings", nil)
	oldRec := httptest.NewRecorder()
	router.ServeHTTP(oldRec, oldReq)
	if oldRec.Code != http.StatusOK {
		t.Fatalf("old mount GET: %d %s", oldRec.Code, oldRec.Body.String())
	}

	newReq := httptest.NewRequest(http.MethodGet, "/newsecret/api/settings", nil)
	newRec := httptest.NewRecorder()
	router.ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusNotFound {
		t.Fatalf("new mount before PUT: %d %s", newRec.Code, newRec.Body.String())
	}

	putBody := `{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/newsecret/","mode":"dev","domain":"panel.example.com","email":"admin@example.com"}`
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, httptest.NewRequest(http.MethodPut, "/oldsecret/api/settings", strings.NewReader(putBody)))
	if putRec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched webBasePath put: %d %s", putRec.Code, putRec.Body.String())
	}
	if !strings.Contains(putRec.Body.String(), "webBasePath cannot change until veil repair") {
		t.Fatalf("error body = %s", putRec.Body.String())
	}

	state.mu.Lock()
	if state.settings.WebBasePath != "/oldsecret/" {
		t.Fatalf("persisted webBasePath = %q", state.settings.WebBasePath)
	}
	settings := state.settings
	state.mu.Unlock()

	afterOld := httptest.NewRecorder()
	router.ServeHTTP(afterOld, httptest.NewRequest(http.MethodGet, "/oldsecret/api/settings", nil))
	if afterOld.Code != http.StatusOK {
		t.Fatalf("old mount after PUT: %d %s", afterOld.Code, afterOld.Body.String())
	}
	afterNew := httptest.NewRecorder()
	router.ServeHTTP(afterNew, httptest.NewRequest(http.MethodGet, "/newsecret/api/settings", nil))
	if afterNew.Code != http.StatusNotFound {
		t.Fatalf("new mount after PUT: %d %s", afterNew.Code, afterNew.Body.String())
	}

	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{ApplyRoot: dir, Settings: settings})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddyJSON := configs[filepath.Join(dir, "generated", "caddy", "config.json")]
	if !strings.Contains(caddyJSON, "/oldsecret/") {
		t.Fatalf("Caddy JSON missing process mount:\n%s", caddyJSON)
	}
	if strings.Contains(caddyJSON, "/newsecret/") {
		t.Fatalf("Caddy JSON retargeted to rejected path:\n%s", caddyJSON)
	}

	sameBody := `{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/oldsecret/","mode":"dev","domain":"panel.example.com","email":"admin@example.com"}`
	sameRec := httptest.NewRecorder()
	router.ServeHTTP(sameRec, httptest.NewRequest(http.MethodPut, "/oldsecret/api/settings", strings.NewReader(sameBody)))
	if sameRec.Code != http.StatusOK {
		t.Fatalf("matching webBasePath put: %d %s", sameRec.Code, sameRec.Body.String())
	}
}

func TestSettingsPutMatchingWebBasePathSucceeds(t *testing.T) {
	r, state := newSettingsEchoRouter(t)
	state.serveWebBasePath = "/oldsecret/"
	state.servePanelListen = "127.0.0.1:2096"
	state.servePanelAccess = "caddy"
	body := `{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/oldsecret/","mode":"dev","domain":"panel.example.com","email":"admin@example.com"}`
	if code := putSettings(t, r, body); code != http.StatusOK {
		t.Fatalf("matching webBasePath put: %d", code)
	}
}
