package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
)

func pinRoutingTestBodies(source RoutingSource, bodies map[string]string) RoutingSource {
	source.Files = append([]generatedconfig.RoutingSourceFile(nil), source.Files...)
	for index := range source.Files {
		body, ok := bodies[source.Files[index].Name]
		if !ok {
			continue
		}
		digest := sha256.Sum256([]byte(body))
		source.Files[index].PinnedSHA256 = hex.EncodeToString(digest[:])
		source.Files[index].SignatureURL = ""
		source.Files[index].CertificateIdentity = ""
		source.Files[index].CertificateOIDCIssuer = ""
	}
	return source
}

func TestManagementApplyStagesRoutingPresetRuleDatFiles(t *testing.T) {
	oldDownloader := routeDatDownloader
	oldVerifier := routeDatSignatureVerifier
	oldTransform := routeDatSourceTransform
	routeDatSourceTransform = func(source RoutingSource) RoutingSource {
		return pinRoutingTestBodies(source, map[string]string{"geoip.dat": "fake geoip dat", "geosite.dat": "fake geosite dat"})
	}
	routeDatSignatureVerifier = func(context.Context, generatedconfig.RoutingSourceFile, []byte, []byte) error { return nil }
	routeDatDownloader = func(_ context.Context, url string) ([]byte, error) {
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
		if strings.HasSuffix(url, ".bundle") {
			return []byte(`{"fake":"sigstore bundle"}`), nil
		}
		return nil, fmt.Errorf("unexpected routing dat URL: %s", url)
	}
	t.Cleanup(func() {
		routeDatDownloader = oldDownloader
		routeDatSignatureVerifier = oldVerifier
		routeDatSourceTransform = oldTransform
	})

	applyRoot := t.TempDir()
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	// Displayed config paths are anchored at the state's live root
	// (<applyRoot>/live here; the packaged default is /etc/veil/generated)
	// so the preview matches where the apply job promotes (issue #636).
	liveRules := filepath.ToSlash(filepath.Join(applyRoot, "live", "rules"))
	if plan.Code != http.StatusOK || !strings.Contains(plan.Body.String(), liveRules+"/geoip.dat") || !strings.Contains(plan.Body.String(), liveRules+"/geosite.dat") {
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
	for _, want := range []string{`"route":`, `"rule_set": "geoip-ru-blocked"`, `"rule_set": "geosite-ru-blocked"`} {
		if !strings.Contains(string(warpConfig), want) {
			t.Fatalf("staged WARP config missing routing preset fragment %q:\n%s", want, string(warpConfig))
		}
	}
}

func TestManagementApplyIncludesHysteria2GeoMatchersOnFirstDatDownload(t *testing.T) {
	oldDownloader := routeDatDownloader
	oldVerifier := routeDatSignatureVerifier
	oldTransform := routeDatSourceTransform
	routeDatSourceTransform = func(source RoutingSource) RoutingSource {
		return pinRoutingTestBodies(source, map[string]string{"geoip.dat": "fake geoip dat", "geosite.dat": "fake geosite dat"})
	}
	routeDatSignatureVerifier = func(context.Context, generatedconfig.RoutingSourceFile, []byte, []byte) error { return nil }
	routeDatDownloader = func(_ context.Context, url string) ([]byte, error) {
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
		if strings.HasSuffix(url, ".bundle") {
			return []byte(`{"fake":"sigstore bundle"}`), nil
		}
		return nil, fmt.Errorf("unexpected routing dat URL: %s", url)
	}
	t.Cleanup(func() {
		routeDatDownloader = oldDownloader
		routeDatSignatureVerifier = oldVerifier
		routeDatSourceTransform = oldTransform
	})

	applyRoot := t.TempDir()
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	settings := httptest.NewRecorder()
	r.ServeHTTP(settings, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","hysteria2Password":"hy2-secret"}`)))
	if settings.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body.String())
	}
	inbound := httptest.NewRecorder()
	r.ServeHTTP(inbound, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true,"password":"hy2-secret"}`)))
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create hysteria2 inbound: %d %s", inbound.Code, inbound.Body.String())
	}
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
	if apply.Code != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %s", apply.Code, apply.Body.String())
	}
	hy2, err := os.ReadFile(filepath.Join(applyRoot, "generated", "hysteria2", "hy2.yaml"))
	if err != nil {
		t.Fatalf("expected staged hysteria2 config: %v", err)
	}
	for _, want := range []string{"proxy(geoip:ru-blocked)", "proxy(geosite:ru-blocked)"} {
		if !strings.Contains(string(hy2), want) {
			t.Fatalf("first-dat-download hysteria2 ACL missing %q:\n%s", want, hy2)
		}
	}
}

func TestManagementApplyRejectsRoutingDatChecksumMismatch(t *testing.T) {
	oldDownloader := routeDatDownloader
	oldVerifier := routeDatSignatureVerifier
	oldTransform := routeDatSourceTransform
	routeDatSourceTransform = func(source RoutingSource) RoutingSource {
		return pinRoutingTestBodies(source, map[string]string{"geoip.dat": "expected geoip dat", "geosite.dat": "fake geosite dat"})
	}
	routeDatSignatureVerifier = func(context.Context, generatedconfig.RoutingSourceFile, []byte, []byte) error { return nil }
	routeDatDownloader = func(_ context.Context, url string) ([]byte, error) {
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
		if strings.HasSuffix(url, ".bundle") {
			return []byte(`{"fake":"sigstore bundle"}`), nil
		}
		return nil, fmt.Errorf("unexpected routing dat URL: %s", url)
	}
	t.Cleanup(func() {
		routeDatDownloader = oldDownloader
		routeDatSignatureVerifier = oldVerifier
		routeDatSourceTransform = oldTransform
	})

	applyRoot := t.TempDir()
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	if apply.Code != http.StatusInternalServerError || responseErrorMessage(t, apply.Body.Bytes()) != "internal server error" || strings.Contains(apply.Body.String(), "checksum mismatch") {
		t.Fatalf("apply expected non-leaking 500, got %d: %s", apply.Code, apply.Body.String())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "rules", "geoip.dat")); !os.IsNotExist(err) {
		t.Fatalf("geoip.dat should not be staged after checksum mismatch, stat err: %v", err)
	}
}

func TestManagementApplyPlanRejectsRoutingRuleUsingDisabledWarpOutbound(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","hysteria2Password":"hy2-secret"},
		"inbounds":[{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}],
		"routingRules":[{"name":"non-ru-through-warp","match":"geosite:geolocation-!ru","outbound":"warp","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","hysteria2Password":"hy2-secret"},
		"inbounds":[{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}],
		"routingRules":[{"name":"bad-outbound","match":"geosite:example","outbound":"shell","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
