package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _routerTestDeps_router_management_apply_test = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestManagementApplyStagesRoutingPresetRuleDatFiles(t *testing.T) {
	oldDownloader := routeDatDownloader
	routeDatDownloader = func(url string) ([]byte, error) {
		if strings.HasSuffix(url, "/geoip.dat") {
			return []byte("fake geoip dat"), nil
		}
		if strings.HasSuffix(url, "/geoip.dat.sha256sum") {
			return []byte(testSHA256Line("fake geoip dat", "geoip.dat")), nil
		}
		if strings.HasSuffix(url, "/geosite.dat") {
			return []byte("fake geosite dat"), nil
		}
		if strings.HasSuffix(url, "/geosite.dat.sha256sum") {
			return []byte(testSHA256Line("fake geosite dat", "geosite.dat")), nil
		}
		return nil, fmt.Errorf("unexpected routing dat URL: %s", url)
	}
	t.Cleanup(func() { routeDatDownloader = oldDownloader })

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	warp := httptest.NewRecorder()
	r.ServeHTTP(warp, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408","privateKey":"warp-private-key","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40000}`)))
	if warp.Code != http.StatusOK {
		t.Fatalf("enable WARP expected 200, got %d: %s", warp.Code, warp.Body.String())
	}
	applyPreset := httptest.NewRecorder()
	r.ServeHTTP(applyPreset, httptest.NewRequest(http.MethodPost, "/api/routing/presets/RU-blocked", nil))
	if applyPreset.Code != http.StatusOK {
		t.Fatalf("apply RU-blocked preset expected 200, got %d: %s", applyPreset.Code, applyPreset.Body.String())
	}

	plan := httptest.NewRecorder()
	r.ServeHTTP(plan, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))
	if plan.Code != http.StatusOK || !strings.Contains(plan.Body.String(), "/etc/veil/generated/rules/geoip.dat") || !strings.Contains(plan.Body.String(), "/etc/veil/generated/rules/geosite.dat") {
		t.Fatalf("apply plan missing routing dat configs, status %d: %s", plan.Code, plan.Body.String())
	}

	apply := httptest.NewRecorder()
	r.ServeHTTP(apply, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
	if apply.Code != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %s", apply.Code, apply.Body.String())
	}
	for path, want := range map[string]string{
		filepath.Join(applyRoot, "generated", "rules", "geoip.dat"):   "fake geoip dat",
		filepath.Join(applyRoot, "generated", "rules", "geosite.dat"): "fake geosite dat",
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected staged routing dat file %s: %v", path, err)
		}
		if string(body) != want {
			t.Fatalf("unexpected staged routing dat body for %s: %q", path, string(body))
		}
	}
	warpConfig, err := os.ReadFile(filepath.Join(applyRoot, "generated", "sing-box", "warp.json"))
	if err != nil {
		t.Fatalf("expected staged WARP config: %v", err)
	}
	for _, want := range []string{`"route":`, `"geoip": "ru-blocked"`, `"geosite": "ru-blocked"`} {
		if !strings.Contains(string(warpConfig), want) {
			t.Fatalf("staged WARP config missing routing preset fragment %q:\n%s", want, string(warpConfig))
		}
	}
}

func TestManagementApplyRejectsRoutingDatChecksumMismatch(t *testing.T) {
	oldDownloader := routeDatDownloader
	routeDatDownloader = func(url string) ([]byte, error) {
		if strings.HasSuffix(url, "/geoip.dat") {
			return []byte("tampered geoip dat"), nil
		}
		if strings.HasSuffix(url, "/geoip.dat.sha256sum") {
			return []byte(testSHA256Line("expected geoip dat", "geoip.dat")), nil
		}
		if strings.HasSuffix(url, "/geosite.dat") {
			return []byte("fake geosite dat"), nil
		}
		if strings.HasSuffix(url, "/geosite.dat.sha256sum") {
			return []byte(testSHA256Line("fake geosite dat", "geosite.dat")), nil
		}
		return nil, fmt.Errorf("unexpected routing dat URL: %s", url)
	}
	t.Cleanup(func() { routeDatDownloader = oldDownloader })

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	warp := httptest.NewRecorder()
	r.ServeHTTP(warp, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408","privateKey":"warp-private-key","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40000}`)))
	if warp.Code != http.StatusOK {
		t.Fatalf("enable WARP expected 200, got %d: %s", warp.Code, warp.Body.String())
	}
	applyPreset := httptest.NewRecorder()
	r.ServeHTTP(applyPreset, httptest.NewRequest(http.MethodPost, "/api/routing/presets/RU-blocked", nil))
	if applyPreset.Code != http.StatusOK {
		t.Fatalf("apply RU-blocked preset expected 200, got %d: %s", applyPreset.Code, applyPreset.Body.String())
	}

	apply := httptest.NewRecorder()
	r.ServeHTTP(apply, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
	if apply.Code != http.StatusInternalServerError || !strings.Contains(apply.Body.String(), "checksum mismatch") {
		t.Fatalf("apply expected checksum mismatch 500, got %d: %s", apply.Code, apply.Body.String())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "rules", "geoip.dat")); !os.IsNotExist(err) {
		t.Fatalf("geoip.dat should not be staged after checksum mismatch, stat err: %v", err)
	}
}

func TestManagementApplyPlanValidatesAndReturnsStagedActions(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Valid {
		t.Fatalf("expected valid plan: %+v", response)
	}
	if len(response.Errors) != 0 {
		t.Fatalf("expected no validation errors: %+v", response.Errors)
	}
	if !containsString(response.Configs, "/etc/veil/generated/caddy/Caddyfile") {
		t.Fatalf("expected caddy config in plan: %+v", response.Configs)
	}
	if !containsString(response.Configs, "/etc/veil/generated/hysteria2/server.yaml") {
		t.Fatalf("expected hysteria2 config in plan: %+v", response.Configs)
	}
	if !containsString(response.Actions, "validate management state") || !containsString(response.Actions, "stage generated configs") || !containsString(response.Actions, "reload veil-naive.service") || !containsString(response.Actions, "reload veil-hysteria2.service") {
		t.Fatalf("expected staged validation/write/reload actions: %+v", response.Actions)
	}
}

func TestManagementApplyPlanRejectsInvalidEnabledInbound(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev"},
		"inbounds":[{"name":"bad","protocol":"hysteria2","transport":"udp","port":0,"enabled":true}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Valid {
		t.Fatalf("expected invalid plan: %+v", response)
	}
	if len(response.Errors) == 0 || !strings.Contains(response.Errors[0], "positive port") {
		t.Fatalf("expected positive port validation error: %+v", response.Errors)
	}
}

func TestManagementApplyPlanHonorsSelectedStack(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"naive","mode":"dev"},
		"inbounds":[
			{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true},
			{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !containsString(response.Configs, "/etc/veil/generated/caddy/Caddyfile") {
		t.Fatalf("expected caddy config in naive stack plan: %+v", response.Configs)
	}
	if containsString(response.Configs, "/etc/veil/generated/hysteria2/server.yaml") || containsString(response.Actions, "reload veil-hysteria2.service") {
		t.Fatalf("did not expect hysteria2 in naive-only stack plan: %+v %+v", response.Configs, response.Actions)
	}
}

func TestManagementApplyPlanRejectsInvalidStack(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"bad","mode":"dev"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Errors) == 0 || !strings.Contains(response.Errors[0], "unsupported stack") {
		t.Fatalf("expected unsupported stack error: %+v", response.Errors)
	}
}

func TestManagementApplyPlanRejectsRoutingRuleUsingDisabledWarpOutbound(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"hysteria2","mode":"dev","domain":"vpn.example.com","hysteria2Password":"hy2-secret"},
		"inbounds":[{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}],
		"routingRules":[{"name":"non-ru-through-warp","match":"geosite:geolocation-!ru","outbound":"warp","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Valid || !strings.Contains(strings.Join(response.Errors, ";"), "requires WARP to be enabled") {
		t.Fatalf("expected disabled WARP routing validation error: %+v", response)
	}
}

func TestManagementApplyPlanRejectsUnknownRoutingOutbound(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"hysteria2","mode":"dev","domain":"vpn.example.com","hysteria2Password":"hy2-secret"},
		"inbounds":[{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}],
		"routingRules":[{"name":"bad-outbound","match":"geosite:example","outbound":"shell","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Valid || !strings.Contains(strings.Join(response.Errors, ";"), "unsupported routing outbound") {
		t.Fatalf("expected unsupported outbound validation error: %+v", response)
	}
}

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
	if !strings.Contains(string(planBody), "reload veil-naive.service") || !strings.Contains(string(planBody), "reload veil-hysteria2.service") {
		t.Fatalf("plan file missing staged actions: %s", string(planBody))
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
			"stack":"both",
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
	caddyPath := filepath.Join(applyRoot, "generated", "caddy", "Caddyfile")
	hy2Path := filepath.Join(applyRoot, "generated", "hysteria2", "server.yaml")
	if !containsString(response.WrittenFiles, caddyPath) || !containsString(response.WrittenFiles, hy2Path) {
		t.Fatalf("apply response missing rendered configs: %+v", response.WrittenFiles)
	}
	caddyBody, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("read caddy config: %v", err)
	}
	if !strings.Contains(string(caddyBody), "vpn.example.com") || !strings.Contains(string(caddyBody), "basic_auth veil naive-secret") || !strings.Contains(string(caddyBody), "protocols h1 h2") {
		t.Fatalf("unexpected caddy config: %s", string(caddyBody))
	}
	hy2Body, err := os.ReadFile(hy2Path)
	if err != nil {
		t.Fatalf("read hysteria2 config: %v", err)
	}
	if !strings.Contains(string(hy2Body), "listen: :443") || !strings.Contains(string(hy2Body), "password: hy2-secret") || !strings.Contains(string(hy2Body), "vpn.example.com") {
		t.Fatalf("unexpected hysteria2 config: %s", string(hy2Body))
	}
}

func TestManagementApplyStagesWarpOutboundWhenEnabled(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"stack":"hysteria2",
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
	for _, want := range []string{`"type": "wireguard"`, `"tag": "warp"`, `"server": "engage.cloudflareclient.com"`, `"server_port": 2408`, `"private_key": "warp-private-key"`, `"type": "socks"`, `"listen_port": 40000`} {
		if !strings.Contains(string(warpBody), want) {
			t.Fatalf("WARP config missing %q: %s", want, string(warpBody))
		}
	}
	if !containsString(response.Plan.Configs, "/etc/veil/generated/sing-box/warp.json") || !containsString(response.Plan.Actions, "reload veil-warp.service") {
		t.Fatalf("plan missing WARP config/action: %+v", response.Plan)
	}
}

func TestManagementApplyPlanRejectsMissingRenderSettingsForEnabledInbound(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","domain":"vpn.example.com"},
		"inbounds":[{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var response ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Valid || len(response.Errors) == 0 || !strings.Contains(strings.Join(response.Errors, ";"), "naive username and password are required") {
		t.Fatalf("expected missing naive credentials validation error: %+v", response)
	}
}

func TestManagementApplyRunsFixedValidatorsForStagedRenderedConfigs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"stack":"both",
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
			{Name: "caddy", Config: filepath.Join(applyRoot, "generated", "caddy", "Caddyfile"), Command: []string{"caddy", "validate", "--config", filepath.Join(applyRoot, "generated", "caddy", "Caddyfile")}, Valid: true},
			{Name: "hysteria2", Config: filepath.Join(applyRoot, "generated", "hysteria2", "server.yaml"), Command: []string{"hysteria", "server", "--config", filepath.Join(applyRoot, "generated", "hysteria2", "server.yaml"), "--check"}, Valid: true},
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
			"stack":"naive",
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

func TestManagementApplyLiveRequiresExplicitFlagAndKeepsStagedOnlyByDefault(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
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
	if _, err := os.Stat(filepath.Join(applyRoot, "live", "caddy", "Caddyfile")); !os.IsNotExist(err) {
		t.Fatalf("staged-only apply should not write live caddy config, stat err: %v", err)
	}
}

func TestManagementApplyLivePromotesValidatedConfigsAndBacksUpExistingFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	existingCaddy := filepath.Join(applyRoot, "live", "caddy", "Caddyfile")
	if err := writeAtomicFile(existingCaddy, []byte("old caddy\n"), 0o600); err != nil {
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
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "Caddyfile")
	liveHysteria := filepath.Join(applyRoot, "live", "hysteria2", "server.yaml")
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
	if !strings.Contains(string(caddyBody), "vpn.example.com") || !strings.Contains(string(caddyBody), "basic_auth veil naive-secret") {
		t.Fatalf("unexpected live caddy config: %s", string(caddyBody))
	}
	if _, err := os.Stat(liveHysteria); err != nil {
		t.Fatalf("expected live hysteria config: %v", err)
	}
}

func TestManagementApplyLiveRejectsFailedValidationBeforeReplacingLiveFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "Caddyfile")
	if err := writeAtomicFile(liveCaddy, []byte("old caddy\n"), 0o600); err != nil {
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
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
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
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
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

func TestManagementApplyServicesRunsAllowlistedReloadsAfterLivePromotion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
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
	expectedNaive := []string{"systemctl", "reload", "veil-naive.service"}
	expectedHy2 := []string{"systemctl", "reload", "veil-hysteria2.service"}
	if !response.ServicesApplied || len(response.ServiceActions) != 2 || len(serviceCalls) != 2 {
		t.Fatalf("expected two service actions: response=%+v calls=%+v", response, serviceCalls)
	}
	if !stringSlicesEqual(serviceCalls[0], expectedNaive) || !stringSlicesEqual(serviceCalls[1], expectedHy2) {
		t.Fatalf("unexpected service calls: %+v", serviceCalls)
	}
	if !response.ServiceActions[0].Success || !response.ServiceActions[1].Success {
		t.Fatalf("expected successful service action results: %+v", response.ServiceActions)
	}
}

func TestManagementApplyServicesStopsOnReloadFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
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
	if response.ServicesApplied || !response.RolledBack || len(response.ServiceActions) != 1 || response.ServiceActions[0].Error != "reload failed" || len(serviceCalls) != 2 {
		t.Fatalf("expected failed service action followed by rollback reload: response=%+v calls=%+v", response, serviceCalls)
	}
}

func TestManagementApplyServicesChecksHealthAfterReload(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
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
	if !stringSlicesEqual(healthCalls[0], []string{"systemctl", "is-active", "--quiet", "veil-naive.service"}) {
		t.Fatalf("unexpected health command: %+v", healthCalls)
	}
}

func TestManagementApplyServicesRollsBackLiveConfigOnHealthFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "Caddyfile")
	if err := writeAtomicFile(liveCaddy, []byte("old caddy\n"), 0o600); err != nil {
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
	expected := [][]string{{"systemctl", "reload", "veil-naive.service"}, {"systemctl", "reload", "veil-naive.service"}}
	if len(serviceCalls) != len(expected) || !stringSlicesEqual(serviceCalls[0], expected[0]) || !stringSlicesEqual(serviceCalls[1], expected[1]) {
		t.Fatalf("expected reload before and after rollback: %+v", serviceCalls)
	}
}

func TestManagementApplyWritesAuditHistoryForSuccessfulServiceApply(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
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

func TestManagementApplyHistoryRetentionKeepsNewestEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	seed := make([]ApplyHistoryEntry, 100)
	for i := range seed {
		seed[i] = ApplyHistoryEntry{ID: fmt.Sprintf("old-%03d", i), Timestamp: fmt.Sprintf("2026-05-01T00:00:%02dZ", i%60), Stage: "staged", Success: true}
	}
	body, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed history: %v", err)
	}
	if err := writeAtomicFile(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"), append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write seed history: %v", err)
	}
	oldValidator := stagedConfigValidator
	defer func() { stagedConfigValidator = oldValidator }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var history []ApplyHistoryEntry
	body, err = os.ReadFile(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 100 {
		t.Fatalf("expected capped history length 100, got %d", len(history))
	}
	if history[0].ID == "" || !history[0].Success || history[0].Stage != "staged" {
		t.Fatalf("expected newest apply entry first, got %+v", history[0])
	}
	if history[len(history)-1].ID != "old-098" {
		t.Fatalf("expected oldest retained entry to be old-098 after trimming old-099, got %+v", history[len(history)-1])
	}
}

func TestManagementApplyHistoryEndpointReturnsNewestFirstAndPersistsAcrossRouters(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	defer func() { stagedConfigValidator = oldValidator }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("apply %d expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	freshRouter, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()
	freshRouter.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var history []ApplyHistoryEntry
	if err := json.NewDecoder(w.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected two history entries, got %+v", history)
	}
	if history[0].ID == history[1].ID || history[0].Timestamp < history[1].Timestamp {
		t.Fatalf("expected unique newest-first entries: %+v", history)
	}
	if history[0].Stage != "staged" || !history[0].Success || history[0].LiveApplied || history[0].ServicesApplied {
		t.Fatalf("unexpected staged history entry: %+v", history[0])
	}
}

func TestManagementApplyHistoryEndpointFiltersStageSuccessAndLimit(t *testing.T) {
	applyRoot := t.TempDir()
	history := []ApplyHistoryEntry{
		{ID: "4", Timestamp: "2026-05-01T00:00:04Z", Stage: "rollback", Success: false},
		{ID: "3", Timestamp: "2026-05-01T00:00:03Z", Stage: "rollback", Success: false},
		{ID: "2", Timestamp: "2026-05-01T00:00:02Z", Stage: "live", Success: true},
		{ID: "1", Timestamp: "2026-05-01T00:00:01Z", Stage: "staged", Success: true},
	}
	body, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if err := writeAtomicFile(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"), append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write history: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history?stage=rollback&success=false&limit=1", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var filtered []ApplyHistoryEntry
	if err := json.NewDecoder(w.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered history: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "4" || filtered[0].Stage != "rollback" || filtered[0].Success {
		t.Fatalf("unexpected filtered history: %+v", filtered)
	}
}

func TestManagementApplyHistoryEndpointRejectsInvalidFilterNamesAndValues(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})
	cases := []string{
		"/api/apply/history?success=maybe",
		"/api/apply/history?limit=-1",
		"/api/apply/history?limit=abc",
		"/api/apply/history?stage=unknown",
		"/api/apply/history?offset=1",
	}
	for _, path := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s expected 400, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}

func TestManagementApplyHistoryEndpointReportsFirstUnsupportedFilterDeterministically(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})

	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history?zzz=1&aaa=1", nil))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unsupported filters, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid history filter: aaa") {
			t.Fatalf("expected deterministic first unsupported filter aaa, got %q", w.Body.String())
		}
	}
}

func TestManagementApplyWritesAuditHistoryForRollback(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	liveCaddy := filepath.Join(applyRoot, "live", "caddy", "Caddyfile")
	if err := writeAtomicFile(liveCaddy, []byte("old caddy\n"), 0o600); err != nil {
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

func TestManagementApplyRejectsInvalidPlanWithoutWritingFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"bad","mode":"dev"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid apply should not write files, stat err: %v", err)
	}
}
