package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterHealthz(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok\n" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestRouterRequiresAuthTokenForAPIWhenConfigured(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Fatalf("expected Bearer challenge, got %q", got)
	}
}

func TestRouterAcceptsBearerAuthTokenForAPIWhenConfigured(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterAcceptsVeilTokenHeaderForAPIWhenConfigured(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	body := strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	req.Header.Set("X-Veil-Token", "secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterLeavesHealthzPublicWhenAuthTokenConfigured(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected public healthz 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterServesPanelShell(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "Veil Panel") || !strings.Contains(body, "/api/status") {
		t.Fatalf("unexpected panel body: %s", body)
	}
}

func TestRURecommendedPreviewEndpoint(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["domain"] != "example.com" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response["caddyfile"] == "" || response["hysteria2YAML"] == "" {
		t.Fatalf("expected rendered configs: %+v", response)
	}
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "preview-naive") || strings.Contains(string(encoded), "preview-hysteria2") || strings.Contains(string(encoded), "preview-panel") {
		t.Fatalf("preview response leaked generated secrets: %s", string(encoded))
	}
	if !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("preview response should include redaction markers: %s", string(encoded))
	}
}

func TestRURecommendedPreviewEndpointHonorsStack(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"naive"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["stack"] != "naive" {
		t.Fatalf("expected stack naive, got %+v", response)
	}
	if response["caddyfile"] == "" || response["hysteria2YAML"] != "" {
		t.Fatalf("expected only naive preview output: %+v", response)
	}
	if response["naiveClientURL"] == "" || response["hysteria2ClientURI"] != "" {
		t.Fatalf("expected only naive client link: %+v", response)
	}
}

func TestRURecommendedPreviewEndpointRejectsInvalidStack(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"bad"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSpeedtestEndpointRunsConfiguredRunner(t *testing.T) {
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{
			Server:       "Test ISP - Moscow",
			PingMS:       12.3,
			DownloadMbps: 101.5,
			UploadMbps:   42.7,
		}, nil
	}
	defer func() { speedtestRunner = old }()

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response SpeedtestResult
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DownloadMbps != 101.5 || response.UploadMbps != 42.7 || response.PingMS != 12.3 {
		t.Fatalf("unexpected speedtest result: %+v", response)
	}
	if response.Server != "Test ISP - Moscow" {
		t.Fatalf("unexpected server: %+v", response)
	}
}

func TestSpeedtestEndpointReportsRunnerError(t *testing.T) {
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{}, errSpeedtestUnavailable
	}
	defer func() { speedtestRunner = old }()

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterServesPanelShellWithSpeedtestControl(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Speedtest") || !strings.Contains(body, "/api/tools/speedtest") {
		t.Fatalf("expected speedtest control in panel shell: %s", body)
	}
}

func TestRouterServesPanelShellWithManagementSections(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"Settings", "Inbounds", "Routing rules", "WARP", "/api/settings", "/api/inbounds", "/api/routing/rules", "/api/warp"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithTokenControls(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"API token", "localStorage", "veil_api_token", "X-Veil-Token"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing auth control %q: %s", want, body)
		}
	}
}

func TestManagementAPIExposesSettingsInboundsRoutingAndWarp(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	cases := []struct {
		path string
		want string
	}{
		{path: "/api/settings", want: "panelListen"},
		{path: "/api/inbounds", want: "naive"},
		{path: "/api/routing/rules", want: "direct"},
		{path: "/api/warp", want: "enabled"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", tc.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s response missing %q: %s", tc.path, tc.want, w.Body.String())
		}
	}
}

func TestManagementAPIUpdatesWarpConfig(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"enabled":true,"licenseKey":"","endpoint":"engage.cloudflareclient.com:2408"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response WarpConfig
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Enabled || response.Endpoint != "engage.cloudflareclient.com:2408" {
		t.Fatalf("unexpected warp config: %+v", response)
	}
}

func TestManagementAPICreatesInbound(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response Inbound
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "hy2-alt" || response.Port != 8443 {
		t.Fatalf("unexpected inbound: %+v", response)
	}
}

func TestManagementAPIPersistsInboundsAndWarpAcrossRouterRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	createInbound := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`))
	createInboundRecorder := httptest.NewRecorder()
	r.ServeHTTP(createInboundRecorder, createInbound)
	if createInboundRecorder.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", createInboundRecorder.Code, createInboundRecorder.Body.String())
	}

	updateWarp := httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408"}`))
	updateWarpRecorder := httptest.NewRecorder()
	r.ServeHTTP(updateWarpRecorder, updateWarp)
	if updateWarpRecorder.Code != http.StatusOK {
		t.Fatalf("update warp expected 200, got %d: %s", updateWarpRecorder.Code, updateWarpRecorder.Body.String())
	}

	restarted := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	inboundsReq := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	inboundsRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(inboundsRecorder, inboundsReq)
	if inboundsRecorder.Code != http.StatusOK {
		t.Fatalf("get inbounds expected 200, got %d: %s", inboundsRecorder.Code, inboundsRecorder.Body.String())
	}
	if !strings.Contains(inboundsRecorder.Body.String(), "hy2-alt") {
		t.Fatalf("persisted inbounds missing hy2-alt: %s", inboundsRecorder.Body.String())
	}

	warpReq := httptest.NewRequest(http.MethodGet, "/api/warp", nil)
	warpRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(warpRecorder, warpReq)
	if warpRecorder.Code != http.StatusOK {
		t.Fatalf("get warp expected 200, got %d: %s", warpRecorder.Code, warpRecorder.Body.String())
	}
	if !strings.Contains(warpRecorder.Body.String(), `"enabled":true`) {
		t.Fatalf("persisted warp missing enabled=true: %s", warpRecorder.Body.String())
	}
}

func TestManagementAPIRejectsDuplicateInboundTransportPort(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"name":"duplicate-naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIUpdatesSettingsAndCreatesRoutingRule(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	settingsReq := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:3000","stack":"naive","mode":"server"}`))
	settingsRecorder := httptest.NewRecorder()
	r.ServeHTTP(settingsRecorder, settingsReq)
	if settingsRecorder.Code != http.StatusOK {
		t.Fatalf("update settings expected 200, got %d: %s", settingsRecorder.Code, settingsRecorder.Body.String())
	}

	routingReq := httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geosite:ru","outbound":"direct","enabled":true}`))
	routingRecorder := httptest.NewRecorder()
	r.ServeHTTP(routingRecorder, routingReq)
	if routingRecorder.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", routingRecorder.Code, routingRecorder.Body.String())
	}

	restarted := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	settingsRead := httptest.NewRecorder()
	restarted.ServeHTTP(settingsRead, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if !strings.Contains(settingsRead.Body.String(), `"stack":"naive"`) || !strings.Contains(settingsRead.Body.String(), `"panelListen":"127.0.0.1:3000"`) {
		t.Fatalf("persisted settings missing updates: %s", settingsRead.Body.String())
	}

	routingRead := httptest.NewRecorder()
	restarted.ServeHTTP(routingRead, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(routingRead.Body.String(), "ru-sites") {
		t.Fatalf("persisted routing rules missing ru-sites: %s", routingRead.Body.String())
	}
}

func TestRouterStatus(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Name != "Veil" || body.Version != "test" || body.Mode != "dev" {
		t.Fatalf("unexpected status: %+v", body)
	}
	if len(body.Services) != 3 {
		t.Fatalf("expected 3 services, got %+v", body.Services)
	}
}
