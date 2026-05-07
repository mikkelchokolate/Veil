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

var _router_management_apply_routing_deps = []any{
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
