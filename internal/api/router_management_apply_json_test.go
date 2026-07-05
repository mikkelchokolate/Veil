package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementApplyRejectsOversizedJSONBody(t *testing.T) {
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	body := strings.NewReader(`{"confirm":true,"note":"` + strings.Repeat("a", 1024*1024+1) + `"}`)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", body))

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized apply body, got %d with response length %d", w.Code, w.Body.Len())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("oversized apply should not write files, stat err: %v", err)
	}
}

func TestManagementApplyRejectsTrailingJSONDataWithoutWritingFiles(t *testing.T) {
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true} {}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing JSON data, got %d with response length %d", w.Code, w.Body.Len())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("trailing JSON apply should not write files, stat err: %v", err)
	}
}

func TestManagementApplyRejectsMalformedJSONWithoutWritingFiles(t *testing.T) {
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{broken`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d with response length %d", w.Code, w.Body.Len())
	}
	body := w.Body.String()
	if strings.Contains(body, "invalid character") || strings.Contains(body, "cannot unmarshal") {
		t.Fatalf("malformed JSON error should not leak decoder internals: %q", body)
	}
	if !strings.Contains(body, "invalid JSON") {
		t.Fatalf("malformed JSON error should be sanitized, got: %q", body)
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("malformed JSON apply should not write files, stat err: %v", err)
	}
}

func TestManagementApplyRequiresConfirmBeforeWritingFiles(t *testing.T) {
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":false}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("apply should not write files without confirm, stat err: %v", err)
	}
}

func TestManagementApplyWritesStagedFilesWhenConfirmed(t *testing.T) {
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	planPath := filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")
	statePath := filepath.Join(applyRoot, "generated", "veil", "management-state.json")
	if !response.Applied || !containsString(response.WrittenFiles, planPath) || !containsString(response.WrittenFiles, statePath) {
		t.Fatalf("unexpected apply response: %+v", response)
	}
	planBody, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	if strings.Contains(string(planBody), "reload veil-caddy@panel.service") || strings.Contains(string(planBody), "reload veil-hysteria2@.service") {
		t.Fatalf("fresh Panel without Inbounds should not stage proxy reloads: %s", string(planBody))
	}
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(stateBody), `"inbounds"`) || !strings.Contains(string(stateBody), `"warp"`) {
		t.Fatalf("state file missing management state: %s", string(stateBody))
	}
}

func TestManagementApplyStagesRenderedConfigsFromManagementState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"mode":"dev",
			"domain":"vpn.example.com",
			"email":"admin@example.com",
			"naiveUsername":"veil",
			"naivePassword":"naive-secret",
			"hysteria2Password":"hy2-secret",
			"masqueradeURL":"https://www.bing.com/",
			"fallbackRoot":"/var/lib/veil/www"
		},
		"inbounds":[
			{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true},
			{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
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
	caddyPath := filepath.Join(applyRoot, "generated", "caddy", "config.json")
	hy2Path := filepath.Join(applyRoot, "generated", "hysteria2", "hysteria2.yaml")
	if !containsString(response.WrittenFiles, caddyPath) || !containsString(response.WrittenFiles, hy2Path) {
		t.Fatalf("apply response missing rendered configs: %+v", response.WrittenFiles)
	}
	caddyBody, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("read caddy config: %v", err)
	}
	if !strings.Contains(string(caddyBody), "vpn.example.com") || !strings.Contains(string(caddyBody), `"handler": "forward_proxy"`) {
		t.Fatalf("unexpected caddy config: %s", string(caddyBody))
	}
	hy2Body, err := os.ReadFile(hy2Path)
	if err != nil {
		t.Fatalf("read hysteria2 config: %v", err)
	}
	// Self-signed TLS now (no ACME/domain in the server config); the cert path
	// is Veil's managed panel cert.
	if !strings.Contains(string(hy2Body), "listen: :443") || !strings.Contains(string(hy2Body), "password: hy2-secret") || !strings.Contains(string(hy2Body), "/etc/veil/panel/tls.crt") {
		t.Fatalf("unexpected hysteria2 config: %s", string(hy2Body))
	}
}

func TestManagementApplyStagesWarpOutboundWhenEnabled(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"mode":"dev",
			"domain":"vpn.example.com",
			"email":"admin@example.com",
			"hysteria2Password":"hy2-secret"
		},
		"inbounds":[{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}],
		"routingRules":[{"name":"non-ru-through-warp","match":"geosite:geolocation-!ru","outbound":"warp","enabled":true}],
		"warp":{
			"enabled":true,
			"endpoint":"engage.cloudflareclient.com:2408",
			"privateKey":"warp-private-key",
			"localAddress":"172.16.0.2/32",
			"peerPublicKey":"warp-peer-key",
			"reserved":[1,2,3],
			"socksListen":"127.0.0.1",
			"socksPort":40000,
			"mtu":1280
		}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
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
	warpPath := filepath.Join(applyRoot, "generated", "sing-box", "warp.json")
	if !containsString(response.WrittenFiles, warpPath) {
		t.Fatalf("apply response missing WARP config: %+v", response.WrittenFiles)
	}
	warpBody, err := os.ReadFile(warpPath)
	if err != nil {
		t.Fatalf("read WARP config: %v", err)
	}
	for _, want := range []string{`"endpoints":`, `"type": "wireguard"`, `"tag": "warp"`, `"address": "engage.cloudflareclient.com"`, `"port": 2408`, `"private_key": "warp-private-key"`, `"type": "socks"`, `"listen_port": 40000`} {
		if !strings.Contains(string(warpBody), want) {
			t.Fatalf("WARP config missing %q: %s", want, string(warpBody))
		}
	}
	if !containsString(response.Plan.Configs, "/etc/veil/generated/sing-box/warp.json") || !containsString(response.Plan.Actions, "restart veil-warp.service") {
		t.Fatalf("plan missing WARP config/action: %+v", response.Plan)
	}
}

func TestManagementApplyRunsFixedValidatorsForStagedRenderedConfigs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"mode":"dev",
			"domain":"vpn.example.com",
			"email":"admin@example.com",
			"naiveUsername":"veil",
			"naivePassword":"naive-secret",
			"hysteria2Password":"hy2-secret"
		},
		"inbounds":[
			{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true},
			{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	old := stagedConfigValidator
	defer func() { stagedConfigValidator = old }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{
			{Name: "caddy", Config: filepath.Join(applyRoot, "generated", "caddy", "config.json"), Command: []string{"caddy", "validate", "--config", filepath.Join(applyRoot, "generated", "caddy", "config.json")}, Valid: true},
			{Name: "hysteria2", Config: filepath.Join(applyRoot, "generated", "hysteria2", "hysteria2.yaml"), Command: []string{"hysteria", "server", "--config", filepath.Join(applyRoot, "generated", "hysteria2", "hysteria2.yaml"), "--check"}, Valid: true},
		}
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
	if len(response.Validations) != 2 {
		t.Fatalf("expected two validation results: %+v", response.Validations)
	}
	if response.Validations[0].Name != "caddy" || !containsString(response.Validations[0].Command, "validate") || response.Validations[1].Name != "hysteria2" || !containsString(response.Validations[1].Command, "--check") {
		t.Fatalf("unexpected fixed validation commands: %+v", response.Validations)
	}
}

func TestManagementApplyReportsValidatorFailureWithoutSystemdSideEffects(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"mode":"dev",
			"domain":"vpn.example.com",
			"email":"admin@example.com",
			"naiveUsername":"veil",
			"naivePassword":"naive-secret"
		},
		"inbounds":[{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	old := stagedConfigValidator
	defer func() { stagedConfigValidator = old }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Command: []string{"caddy", "validate", "--config", paths[0]}, Valid: false, Error: "caddy validation failed"}}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected staged apply response despite validation report, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Validations) != 1 || response.Validations[0].Valid || response.Validations[0].Error != "caddy validation failed" {
		t.Fatalf("expected validation failure result: %+v", response.Validations)
	}
	if !containsString(response.Plan.Actions, "stage generated configs") || containsString(response.Plan.Actions, "systemctl restart") {
		t.Fatalf("staged apply should not include systemd side effects: %+v", response.Plan.Actions)
	}
}
