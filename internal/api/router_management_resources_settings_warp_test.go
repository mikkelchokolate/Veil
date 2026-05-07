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

var _router_management_resources_settings_warp_deps = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestManagementAPIExposesSettingsInboundsRoutingAndWarp(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

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
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"enabled":true,"licenseKey":"","endpoint":"engage.cloudflareclient.com:2408","privateKey":"warp-private-key","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40000}`)
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
	if !response.Enabled || response.Endpoint != "engage.cloudflareclient.com:2408" || response.SocksPort != 40000 {
		t.Fatalf("unexpected warp config: %+v", response)
	}
	if response.PrivateKey != "[REDACTED]" {
		t.Fatalf("WARP private key should be redacted in API response: %+v", response)
	}
}

func TestManagementAPIWarpPutRejectsOversizedJSONBody(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408","privateKey":"` + strings.Repeat("a", 1024*1024+1) + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized WARP body, got %d with response length %d", w.Code, w.Body.Len())
	}
}

func TestManagementAPIWarpPutRejectsUnknownJSONFields(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408","typo":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown JSON field, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `unknown field "typo"`) {
		t.Fatalf("expected unknown field diagnostic, got %q", body)
	}
}

func TestManagementAPIWarpPutPreservesRedactedSecrets(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"licenseKey":"warp-license","endpoint":"engage.cloudflareclient.com:2408","privateKey":"warp-private-key","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40000}`)))
	if create.Code != http.StatusOK {
		t.Fatalf("initial warp update expected 200, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"licenseKey":"[REDACTED]","endpoint":"162.159.193.10:2408","privateKey":"[REDACTED]","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40001}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("redacted warp update expected 200, got %d: %s", update.Code, update.Body.String())
	}

	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"licenseKey": "warp-license"`, `"privateKey": "warp-private-key"`, `"socksPort": 40001`, `"endpoint": "162.159.193.10:2408"`} {
		if !strings.Contains(string(stateBody), want) {
			t.Fatalf("persisted WARP state missing %q after redacted update: %s", want, string(stateBody))
		}
	}
}

func TestManagementAPISettingsPutRejectsOversizedJSONBody(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","domain":"` + strings.Repeat("a", 1024*1024+1) + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized settings body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPISettingsResponsesRedactSecrets(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret","hysteria2Password":"hy2-secret"}`)
	put := httptest.NewRecorder()

	r.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/settings", body))

	if put.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", put.Code, put.Body.String())
	}
	if strings.Contains(put.Body.String(), "naive-secret") || strings.Contains(put.Body.String(), "hy2-secret") || !strings.Contains(put.Body.String(), "[REDACTED]") {
		t.Fatalf("PUT /api/settings leaked secrets: %s", put.Body.String())
	}
	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "naive-secret") || strings.Contains(get.Body.String(), "hy2-secret") || !strings.Contains(get.Body.String(), "[REDACTED]") {
		t.Fatalf("GET /api/settings leaked secrets: %s", get.Body.String())
	}
}

func TestManagementAPISettingsPutPreservesRedactedSecrets(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret","hysteria2Password":"hy2-secret"}`)))
	if create.Code != http.StatusOK {
		t.Fatalf("initial settings update expected 200, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:3096","stack":"naive","mode":"server","domain":"vpn2.example.com","email":"ops@example.com","naiveUsername":"veil2","naivePassword":"[REDACTED]","hysteria2Password":"[REDACTED]"}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("redacted settings update expected 200, got %d: %s", update.Code, update.Body.String())
	}

	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"naivePassword": "naive-secret"`, `"hysteria2Password": "hy2-secret"`, `"panelListen": "127.0.0.1:3096"`, `"domain": "vpn2.example.com"`} {
		if !strings.Contains(string(stateBody), want) {
			t.Fatalf("persisted settings state missing %q after redacted update: %s", want, string(stateBody))
		}
	}
}

func TestManagementAPISettingsPutRejectsInvalidStack(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	invalidStacks := []string{"invalid", "NATIVE", "BOTH", "hysteria", " ", "naiveproxy"}
	for _, stack := range invalidStacks {
		t.Run(stack, func(t *testing.T) {
			body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"` + stack + `","mode":"dev"}`)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid stack %q, got %d: %s", stack, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "stack must be naive, hysteria2, or both") {
				t.Fatalf("expected stack validation error for %q, got: %s", stack, w.Body.String())
			}
		})
	}
}

func TestManagementAPIPersistsInboundsAndWarpAcrossRouterRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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

func TestManagementAPIUpdatesSettingsAndCreatesRoutingRule(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
