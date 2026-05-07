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

var _routerTestDeps_router_management_resources_test = []any{
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

func TestManagementAPICreatesInbound(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	if ct := w.Result().Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content-type on 201, got %q", ct)
	}
	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on 201, got %q", cc)
	}
	if nosniff := w.Result().Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on 201, got %q", nosniff)
	}
}

func TestManagementAPIInboundsRejectOversizedJSONBodies(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	oversizedName := strings.Repeat("a", 1024*1024+1)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/inbounds",
			body:   `{"name":"` + oversizedName + `","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/api/inbounds/naive",
			body:   `{"protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true,"path":"` + oversizedName + `"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 for oversized inbound body, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestManagementAPIRejectsDuplicateInboundName(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":8443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate inbound name, got %d: %s", w.Code, w.Body.String())
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

func TestManagementAPIRejectsDuplicateInboundTransportPort(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"name":"duplicate-naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIUpdatesAndDeletesInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/hy2-alt", strings.NewReader(`{"protocol":"hysteria2","transport":"udp","port":9443,"enabled":false}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("update inbound expected 200, got %d: %s", update.Code, update.Body.String())
	}
	var updated Inbound
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated inbound: %v", err)
	}
	if updated.Name != "hy2-alt" || updated.Port != 9443 || updated.Enabled {
		t.Fatalf("unexpected updated inbound: %+v", updated)
	}

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	readAfterUpdate := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterUpdate, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
	if !strings.Contains(readAfterUpdate.Body.String(), `"port":9443`) || strings.Contains(readAfterUpdate.Body.String(), `"port":8443`) {
		t.Fatalf("persisted inbound update missing: %s", readAfterUpdate.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/inbounds/hy2-alt", nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete inbound expected 204, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	readAfterDelete := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterDelete, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
	if strings.Contains(readAfterDelete.Body.String(), "hy2-alt") {
		t.Fatalf("deleted inbound still present: %s", readAfterDelete.Body.String())
	}
}

func TestManagementAPIRejectsInboundUpdateToDuplicateTransportPort(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/hysteria2", strings.NewReader(`{"protocol":"hysteria2","transport":"tcp","port":443,"enabled":true}`)))
	if update.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port on update, got %d: %s", update.Code, update.Body.String())
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

func TestManagementAPIRoutingRulesRejectOversizedJSONBodies(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	oversizedMatch := "geosite:" + strings.Repeat("a", 1024*1024+1)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/routing/rules",
			body:   `{"name":"huge-rule","match":"` + oversizedMatch + `","outbound":"direct","enabled":true}`,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/api/routing/rules/default-direct",
			body:   `{"match":"` + oversizedMatch + `","outbound":"direct","enabled":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 for oversized routing rule body, got %d with response length %d", w.Code, w.Body.Len())
			}
		})
	}
}

func TestManagementAPIUpdatesAndDeletesRoutingRuleByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"non-ru","match":"geosite:geolocation-!ru","outbound":"warp","enabled":false}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/routing/rules/non-ru", strings.NewReader(`{"match":"geosite:openai","outbound":"direct","enabled":true}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("update routing rule expected 200, got %d: %s", update.Code, update.Body.String())
	}
	var updated RoutingRule
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated rule: %v", err)
	}
	if updated.Name != "non-ru" || updated.Match != "geosite:openai" || updated.Outbound != "direct" || !updated.Enabled {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	readAfterUpdate := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterUpdate, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(readAfterUpdate.Body.String(), "geosite:openai") {
		t.Fatalf("persisted routing rule update missing: %s", readAfterUpdate.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/routing/rules/non-ru", nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete routing rule expected 204, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	readAfterDelete := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterDelete, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if strings.Contains(readAfterDelete.Body.String(), "non-ru") {
		t.Fatalf("deleted routing rule still present: %s", readAfterDelete.Body.String())
	}
}

func TestManagementAPIRejectsDuplicateRoutingRuleName(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geosite:ru","outbound":"direct","enabled":true}`)))
	if first.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", first.Code, first.Body.String())
	}

	duplicate := httptest.NewRecorder()
	r.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geoip:ru","outbound":"direct","enabled":true}`)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate routing rule expected 409, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestManagementAPIGetsRoutingRuleByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geosite:ru","outbound":"direct","enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", create.Code, create.Body.String())
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/rules/ru-sites", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET named routing rule, got %d: %s", w.Code, w.Body.String())
	}
	var response RoutingRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "ru-sites" || response.Match != "geosite:ru" || response.Outbound != "direct" || !response.Enabled {
		t.Fatalf("unexpected routing rule: %+v", response)
	}
}

func TestManagementAPIGetRoutingRuleByNameReturnsNotFoundForMissing(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/rules/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing routing rule, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIExposesRoutingPresetProfiles(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/presets", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("routing presets expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"all", "all-except-Russia", "RU-blocked", "runetfreedom/russia-v2ray-rules-dat", "geoip.dat", "geoip.dat.sha256sum", "geosite.dat", "geosite.dat.sha256sum"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("routing presets response missing %q: %s", want, w.Body.String())
		}
	}
}

func TestManagementAPIAppliesRoutingPresetAndPersistsRules(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/routing/presets/all-except-Russia", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("apply preset expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"all-except-Russia", "geoip:ru", "geosite:category-ru", `"match":"all"`, `"outbound":"warp"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("preset response missing %q: %s", want, w.Body.String())
		}
	}

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	read := httptest.NewRecorder()
	restarted.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(read.Body.String(), "preset-all-except-russia") || !strings.Contains(read.Body.String(), "geosite:category-ru") {
		t.Fatalf("persisted preset routing rules missing: %s", read.Body.String())
	}
}

func TestManagementAPIRejectsUnknownRoutingPreset(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/routing/presets/not-real", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown routing preset expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on API 404, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on API 404, got %q", nosniff)
	}
}

func TestManagementAPIGetsInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", create.Code, create.Body.String())
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds/hy2-alt", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET named inbound, got %d: %s", w.Code, w.Body.String())
	}
	var response Inbound
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "hy2-alt" || response.Port != 8443 || response.Protocol != "hysteria2" {
		t.Fatalf("unexpected inbound: %+v", response)
	}
}

func TestManagementAPIGetInboundByNameReturnsNotFoundForMissing(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing inbound, got %d: %s", w.Code, w.Body.String())
	}
}
