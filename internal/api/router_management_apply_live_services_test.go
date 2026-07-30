package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func TestManagementApplyLiveRequiresExplicitFlagAndKeepsStagedOnlyByDefault(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	old := stagedConfigValidator
	defer func() { stagedConfigValidator = old }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.LiveApplied {
		t.Fatalf("live apply should be false unless applyLive=true: %+v", response)
	}
	if len(response.LiveFiles) != 0 || len(response.BackupFiles) != 0 {
		t.Fatalf("staged-only apply should not report live files/backups: %+v", response)
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "live", "caddy", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("staged-only apply should not write live caddy config, stat err: %v", err)
	}
}

func TestManagementApplyLivePromotesValidatedConfigsAndBacksUpExistingFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	existingCaddy := filepath.Join(applyRoot, "live", "caddy", "config.json")
	if err := atomicfile.Write(existingCaddy, []byte("old caddy\n"), 0o600, 0o700); err != nil {
		t.Fatalf("write existing live caddy: %v", err)
	}
	old := stagedConfigValidator
	defer func() { stagedConfigValidator = old }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "config.json")
	liveHysteria := filepath.Join(applyRoot, "live", "hysteria2", "hysteria2.yaml")
	if !response.LiveApplied || !containsString(response.LiveFiles, liveCaddy) || !containsString(response.LiveFiles, liveHysteria) {
		t.Fatalf("expected live files in response: %+v", response)
	}
	if len(response.BackupFiles) != 1 {
		t.Fatalf("expected one backup for existing caddy config: %+v", response.BackupFiles)
	}
	backupBody, err := os.ReadFile(response.BackupFiles[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupBody) != "old caddy\n" {
		t.Fatalf("unexpected backup body: %q", string(backupBody))
	}
	caddyBody, err := os.ReadFile(liveCaddy)
	if err != nil {
		t.Fatalf("read live caddy: %v", err)
	}
	if !strings.Contains(string(caddyBody), "vpn.example.com") || !strings.Contains(string(caddyBody), "forward_proxy") || !strings.Contains(string(caddyBody), "auth_credentials") {
		t.Fatalf("unexpected live caddy config: %s", string(caddyBody))
	}
	if _, err := os.Stat(liveHysteria); err != nil {
		t.Fatalf("expected live hysteria config: %v", err)
	}
}

func TestManagementApplyLiveRejectsFailedValidationBeforeReplacingLiveFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "config.json")
	if err := atomicfile.Write(liveCaddy, []byte("old caddy\n"), 0o600, 0o700); err != nil {
		t.Fatalf("write existing live caddy: %v", err)
	}
	old := stagedConfigValidator
	defer func() { stagedConfigValidator = old }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: false, Error: "invalid caddy"}}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.LiveApplied || len(response.LiveFiles) != 0 || len(response.BackupFiles) != 0 {
		t.Fatalf("failed validation must not promote live files: %+v", response)
	}
	body, err := os.ReadFile(liveCaddy)
	if err != nil {
		t.Fatalf("read live caddy: %v", err)
	}
	if string(body) != "old caddy\n" {
		t.Fatalf("live caddy was modified despite failed validation: %q", string(body))
	}
}

func TestManagementApplyDoesNotRunServiceActionsWithoutExplicitFlag(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Command: command, Success: true}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ServicesApplied || len(response.ServiceActions) != 0 || len(serviceCalls) != 0 {
		t.Fatalf("service actions should not run without applyServices=true: response=%+v calls=%+v", response, serviceCalls)
	}
}

func TestManagementApplyServicesRequiresLiveApply(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		t.Fatalf("service action should not run when applyLive=false: %+v", command)
		return ServiceActionResult{}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyServices":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ServicesApplied || len(response.ServiceActions) != 0 {
		t.Fatalf("service actions must not run without live promotion: %+v", response)
	}
}

func TestManagementApplyServicesRestartsMieruAfterLivePromotion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableMieruManagementState(statePath); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		if len(paths) != 1 || !strings.Contains(paths[0], filepath.Join("generated", "mieru", "server_config.json")) {
			t.Fatalf("expected only staged Mieru config, got %+v", paths)
		}
		return []ConfigValidationResult{{Name: "mieru", Config: paths[0], Valid: true}}
	}
	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	healthCalls := []string{}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		healthCalls = append(healthCalls, service)
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: true}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	liveMieru := filepath.Join(applyRoot, "live", "mieru", "server_config.json")
	if !response.LiveApplied || !response.ServicesApplied || !containsString(response.LiveFiles, liveMieru) {
		t.Fatalf("expected live Mieru services apply: %+v", response)
	}
	if len(serviceCalls) != 1 || !stringSlicesEqual(serviceCalls[0], []string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatalf("unexpected Mieru service calls: %+v", serviceCalls)
	}
	if len(healthCalls) != 1 || healthCalls[0] != "veil-mieru.service" {
		t.Fatalf("unexpected Mieru health checks: %+v", healthCalls)
	}
	body, err := os.ReadFile(liveMieru)
	if err != nil {
		t.Fatalf("read live Mieru config: %v", err)
	}
	for _, want := range []string{`"protocol": "TCP"`, `"password": "mieru-secret"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("live Mieru config missing %q:\n%s", want, string(body))
		}
	}
}

func TestManagementApplyServicesRunsAllowlistedReloadsAfterLivePromotion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: true}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expectedCaddyAdminLoad := []string{"caddy", "admin", "load"}
	expectedHy2 := []string{"systemctl", "restart", "veil-hysteria2@hysteria2.service"}
	if !response.ServicesApplied || len(response.ServiceActions) != 2 || len(serviceCalls) != 1 {
		t.Fatalf("expected caddy admin load + hysteria2 restart and one systemctl call: response=%+v calls=%+v", response, serviceCalls)
	}
	if !stringSlicesEqual(response.ServiceActions[0].Command, expectedCaddyAdminLoad) {
		t.Fatalf("expected caddy admin load action: %+v", response.ServiceActions)
	}
	if !stringSlicesEqual(serviceCalls[0], expectedHy2) {
		t.Fatalf("unexpected service calls: %+v", serviceCalls)
	}
	if !response.ServiceActions[0].Success || !response.ServiceActions[1].Success {
		t.Fatalf("expected successful service action results: %+v", response.ServiceActions)
	}
}

func TestManagementApplyServicesStopsOnReloadFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: false, Error: "reload failed"}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return fmt.Errorf("caddy admin api unavailable") }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The forward Caddy load succeeds, the following Hysteria2 restart fails,
	// and the transaction rolls back the already-published files. Re-applying
	// the previous Caddy state probes the unit and attempts to start it because
	// the injected runner reports every systemd command as failed.
	if response.ServicesApplied || !response.RolledBack || len(response.ServiceActions) != 2 || len(response.RollbackActions) != 1 {
		t.Fatalf("expected failed service action followed by Caddy rollback: response=%+v calls=%+v", response, serviceCalls)
	}
	wantCalls := [][]string{
		{"systemctl", "restart", "veil-hysteria2@hysteria2.service"},
		{"systemctl", "is-active", "veil-caddy.service"},
		{"systemctl", "start", "veil-caddy.service"},
	}
	if !reflect.DeepEqual(serviceCalls, wantCalls) {
		t.Fatalf("unexpected forward/rollback service calls: got=%+v want=%+v", serviceCalls, wantCalls)
	}
}

func TestManagementApplyServicesChecksHealthAfterReload(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	healthCalls := [][]string{}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		healthCalls = append(healthCalls, []string{"systemctl", "is-active", "--quiet", service})
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: true}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.HealthChecks) != 1 || !response.HealthChecks[0].Healthy || len(healthCalls) != 1 {
		t.Fatalf("expected one successful health check: response=%+v calls=%+v", response.HealthChecks, healthCalls)
	}
	if !stringSlicesEqual(healthCalls[0], []string{"systemctl", "is-active", "--quiet", "veil-caddy.service"}) {
		t.Fatalf("unexpected health command: %+v", healthCalls)
	}
}

func TestManagementApplyServicesRollsBackLiveConfigOnHealthFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "config.json")
	if err := atomicfile.Write(liveCaddy, []byte("old caddy\n"), 0o600, 0o700); err != nil {
		t.Fatalf("write existing live caddy: %v", err)
	}
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: false, Error: "service unhealthy"}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ServicesApplied || !response.RolledBack || len(response.RollbackFiles) != 1 || len(response.RollbackActions) != 1 {
		t.Fatalf("expected rollback response after failed health check: %+v", response)
	}
	body, err := os.ReadFile(liveCaddy)
	if err != nil {
		t.Fatalf("read live caddy: %v", err)
	}
	if string(body) != "old caddy\n" {
		t.Fatalf("expected rollback to restore old live config, got %q", string(body))
	}
	wantRollbackCalls := [][]string{
		{"systemctl", "is-active", "veil-caddy.service"},
		{"systemctl", "reload", "veil-caddy.service"},
	}
	if !reflect.DeepEqual(serviceCalls, wantRollbackCalls) {
		t.Fatalf("unexpected bounded Caddy rollback calls: got=%+v want=%+v", serviceCalls, wantRollbackCalls)
	}
}

func TestManagementApplyWritesAuditHistoryForSuccessfulServiceApply(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: true}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	historyPath := filepath.Join(applyRoot, "generated", "veil", "apply-history.json")
	body, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	if strings.Contains(string(body), "naive-secret") || strings.Contains(string(body), "hy2-secret") {
		t.Fatalf("history must not leak proxy secrets: %s", string(body))
	}
	var history []ApplyHistoryEntry
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected one history entry, got %+v", history)
	}
	entry := history[0]
	if entry.ID == "" || entry.Timestamp == "" || !entry.Success || entry.Stage != "services" || !entry.Applied || !entry.LiveApplied || !entry.ServicesApplied || entry.RolledBack {
		t.Fatalf("unexpected history entry: %+v", entry)
	}
	if len(entry.WrittenFiles) == 0 || len(entry.LiveFiles) != 1 || len(entry.ServiceActions) != 1 || len(entry.HealthChecks) != 1 {
		t.Fatalf("history entry missing apply details: %+v", entry)
	}
}

func TestManagementApplyWritesAuditHistoryForRollback(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "config.json")
	if err := atomicfile.Write(liveCaddy, []byte("old caddy\n"), 0o600, 0o700); err != nil {
		t.Fatalf("write live caddy: %v", err)
	}
	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
	}()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}
	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: false, Error: "service unhealthy"}
	}
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var history []ApplyHistoryEntry
	body, err := os.ReadFile(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 || history[0].Success || history[0].Stage != "rollback" || !history[0].RolledBack || len(history[0].RollbackFiles) != 1 || len(history[0].RollbackActions) != 1 {
		t.Fatalf("expected rollback history entry: %+v", history)
	}
}

func TestManagementApplyServicesCleansOrphanedInstances(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()

	// Write the orphan config files to the live directories
	orphanCaddy := filepath.Join(applyRoot, "live", "caddy", "orphan.json")
	orphanHysteria2 := filepath.Join(applyRoot, "live", "hysteria2", "orphan.yaml")
	if err := atomicfile.Write(orphanCaddy, []byte("caddy config"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan caddy: %v", err)
	}
	if err := atomicfile.Write(orphanHysteria2, []byte("hysteria2 config"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
	}

	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	oldCaddyLoader := caddyAdminLoader
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
		caddyAdminLoader = oldCaddyLoader
	}()

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}

	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}

	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: true}
	}

	caddyAdminLoader = func(_ []byte) error { return nil }

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify that orphans were removed
	if _, err := os.Stat(orphanCaddy); !os.IsNotExist(err) {
		t.Fatalf("orphan caddy config should be deleted, stat err: %v", err)
	}
	if _, err := os.Stat(orphanHysteria2); !os.IsNotExist(err) {
		t.Fatalf("orphan hysteria2 config should be deleted, stat err: %v", err)
	}

	// Verify the service actions (stop/disable for hysteria2 orphan only; caddy is
	// now a single consolidated unit, so leftover caddy configs have no per-instance
	// service to stop).
	expectedStopDisableCalls := [][]string{
		{"systemctl", "stop", "veil-hysteria2@orphan.service"},
		{"systemctl", "disable", "veil-hysteria2@orphan.service"},
	}

	for _, expectedCall := range expectedStopDisableCalls {
		found := false
		for _, call := range serviceCalls {
			if stringSlicesEqual(call, expectedCall) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected service call %v not found in service calls: %+v", expectedCall, serviceCalls)
		}
	}
}

func TestManagementApplyServicesRestoresOrphanedInstancesOnRollback(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()

	// Write the orphan config files to the live directories
	orphanCaddy := filepath.Join(applyRoot, "live", "caddy", "orphan.json")
	orphanHysteria2 := filepath.Join(applyRoot, "live", "hysteria2", "orphan.yaml")
	if err := atomicfile.Write(orphanCaddy, []byte("caddy config"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan caddy: %v", err)
	}
	if err := atomicfile.Write(orphanHysteria2, []byte("hysteria2 config"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
	}

	oldValidator := stagedConfigValidator
	oldRunner := serviceActionRunner
	oldHealth := serviceHealthChecker
	oldCaddyLoader := caddyAdminLoader
	defer func() {
		stagedConfigValidator = oldValidator
		serviceActionRunner = oldRunner
		serviceHealthChecker = oldHealth
		caddyAdminLoader = oldCaddyLoader
	}()

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}

	serviceCalls := [][]string{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		serviceCalls = append(serviceCalls, append([]string(nil), command...))
		return ServiceActionResult{Name: command[len(command)-1], Command: command, Success: true}
	}

	// Make the health checker fail to trigger a rollback
	serviceHealthChecker = func(service string) ServiceHealthResult {
		return ServiceHealthResult{Name: service, Command: []string{"systemctl", "is-active", "--quiet", service}, Healthy: false, Error: "forced reload failure"}
	}

	caddyAdminLoader = func(_ []byte) error { return nil }

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Verify that orphans were restored
	assertFileBody(t, orphanCaddy, "caddy config")
	assertFileBody(t, orphanHysteria2, "hysteria2 config")

	// Verify the service actions on rollback (enable/start for hysteria2 orphan only;
	// caddy is a single consolidated unit with no per-instance orphan service).
	expectedEnableStartCalls := [][]string{
		{"systemctl", "enable", "veil-hysteria2@orphan.service"},
		{"systemctl", "start", "veil-hysteria2@orphan.service"},
	}

	for _, expectedCall := range expectedEnableStartCalls {
		found := false
		for _, call := range serviceCalls {
			if stringSlicesEqual(call, expectedCall) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected rollback call %v not found in service calls: %+v", expectedCall, serviceCalls)
		}
	}
}
