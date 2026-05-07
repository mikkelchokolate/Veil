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

var _router_management_apply_plan_deps = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
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
	if len(response.Configs) != 0 {
		t.Fatalf("fresh Panel without Inbounds should not plan generated proxy configs: %+v", response.Configs)
	}
	if !containsString(response.Actions, "validate management state") || containsString(response.Actions, "reload veil-naive.service") || containsString(response.Actions, "reload veil-hysteria2.service") {
		t.Fatalf("expected only management validation action for fresh Panel: %+v", response.Actions)
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
