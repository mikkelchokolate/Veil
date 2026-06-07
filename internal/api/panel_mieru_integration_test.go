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

func TestPanelHTMLExposesCompleteMieruOperations(t *testing.T) {
	html := panelHTML("/", "", "en")
	for _, want := range []string{
		`<option value="mieru">mieru</option>`,
		`"mieru":["tcp","udp"]`,
		`download-mieru-configs`,
		`mieru-client-configs.json`,
		`restart-mieru`,
		`/api/services/mieru/restart`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Panel HTML missing Mieru integration %q", want)
		}
	}
}

func TestPanelManagementFlowForMieruInboundClientAccessAndApply(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	applyRoot := filepath.Join(dir, "apply")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, KeyPath: keyPath, ApplyRoot: applyRoot})

	putJSON(t, r, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`, http.StatusOK)
	putJSON(t, r, "/api/inbounds", `{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}`, http.StatusCreated)
	putJSON(t, r, "/api/inbounds", `{"name":"mieru-udp","protocol":"mieru","transport":"udp","port":443,"enabled":true,"password":"udp-pass"}`, http.StatusCreated)

	linksRecorder := httptest.NewRecorder()
	r.ServeHTTP(linksRecorder, httptest.NewRequest(http.MethodGet, "/api/client-links", nil))
	if linksRecorder.Code != http.StatusOK {
		t.Fatalf("client links expected 200, got %d: %s", linksRecorder.Code, linksRecorder.Body.String())
	}
	var links ClientLinksResponse
	if err := json.NewDecoder(linksRecorder.Body).Decode(&links); err != nil {
		t.Fatalf("decode client links: %v", err)
	}
	if links.Count != 2 || len(links.Artifacts) != 2 {
		t.Fatalf("expected two Mieru links/artifacts, got %+v", links)
	}
	for _, artifact := range links.Artifacts {
		if artifact.Protocol != "mieru" || artifact.Kind != "client_config" || !strings.Contains(artifact.Content, "vpn.example.com") {
			t.Fatalf("bad Mieru artifact: %+v", artifact)
		}
	}

	subscriptionRecorder := httptest.NewRecorder()
	r.ServeHTTP(subscriptionRecorder, httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=raw", nil))
	if subscriptionRecorder.Code != http.StatusOK {
		t.Fatalf("subscription expected 200, got %d: %s", subscriptionRecorder.Code, subscriptionRecorder.Body.String())
	}
	sub := strings.TrimSpace(subscriptionRecorder.Body.String())
	// Mieru now exports an importable mierus:// URI, so it belongs in the subscription...
	if !strings.Contains(sub, "mierus://") {
		t.Fatalf("Mieru mierus:// URIs must appear in the URI subscription: %q", sub)
	}
	// ...but the raw JSON client config must never leak into the URI subscription.
	if strings.Contains(sub, "portBindings") || strings.Contains(sub, "profileName") || strings.Contains(sub, "{") {
		t.Fatalf("Mieru JSON config must not leak into URI subscription: %q", sub)
	}

	planRecorder := httptest.NewRecorder()
	r.ServeHTTP(planRecorder, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))
	if planRecorder.Code != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %s", planRecorder.Code, planRecorder.Body.String())
	}
	var plan ApplyPlanResponse
	if err := json.NewDecoder(planRecorder.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if !plan.Valid || !containsString(plan.Configs, "/etc/veil/generated/mieru/server_config.json") || !containsString(plan.Actions, "restart veil-mieru.service") {
		t.Fatalf("plan missing Mieru config/action: %+v", plan)
	}

	applyRecorder := httptest.NewRecorder()
	r.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
	if applyRecorder.Code != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %s", applyRecorder.Code, applyRecorder.Body.String())
	}
	configPath := filepath.Join(applyRoot, "generated", "mieru", "server_config.json")
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated Mieru config: %v", err)
	}
	for _, want := range []string{`"protocol": "TCP"`, `"protocol": "UDP"`, `"name": "alice"`, `"password": "udp-pass"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("generated Mieru config missing %q:\n%s", want, string(body))
		}
	}
}

func putJSON(t *testing.T, handler http.Handler, path, body string, wantCode int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if path == "/api/settings" {
		req = httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	}
	handler.ServeHTTP(w, req)
	if w.Code != wantCode {
		t.Fatalf("%s expected %d, got %d: %s", path, wantCode, w.Code, w.Body.String())
	}
}
