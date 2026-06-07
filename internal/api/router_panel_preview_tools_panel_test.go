package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterServesPanelShell(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r, _ := NewRouter(ServerInfo{Version: "test"})
			req := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Fatalf("unexpected content-type: %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("expected no-store cache-control for token-bearing panel shell, got %q", cc)
			}
			if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
				t.Fatalf("expected no-cache Pragma for token-bearing panel shell, got %q", pragma)
			}
			if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Fatalf("expected nosniff for panel shell, got %q", nosniff)
			}
			if csp := w.Header().Get("Content-Security-Policy"); csp != "default-src 'self'; img-src 'self' data: blob: https://api.qrserver.com; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'" {
				t.Fatalf("unexpected panel content-security-policy: %q", csp)
			}
			if referrer := w.Header().Get("Referrer-Policy"); referrer != "no-referrer" {
				t.Fatalf("unexpected panel referrer-policy: %q", referrer)
			}
			if xfo := w.Header().Get("X-Frame-Options"); xfo != "DENY" {
				t.Fatalf("unexpected panel x-frame-options: %q", xfo)
			}
			if permissions := w.Header().Get("Permissions-Policy"); permissions != "camera=(), microphone=(), geolocation=(), payment=(), usb=()" {
				t.Fatalf("unexpected panel permissions-policy: %q", permissions)
			}
			if coop := w.Header().Get("Cross-Origin-Opener-Policy"); coop != "same-origin" {
				t.Fatalf("unexpected panel cross-origin-opener-policy: %q", coop)
			}
			if corp := w.Header().Get("Cross-Origin-Resource-Policy"); corp != "same-origin" {
				t.Fatalf("unexpected panel cross-origin-resource-policy: %q", corp)
			}
			if oac := w.Header().Get("Origin-Agent-Cluster"); oac != "?1" {
				t.Fatalf("unexpected panel origin-agent-cluster: %q", oac)
			}
			if method == http.MethodGet {
				if body := w.Body.String(); !strings.Contains(body, "Veil Panel") || !strings.Contains(body, "/api/version") || !strings.Contains(body, "/api/status") || !strings.Contains(body, "/api/firewall") || !strings.Contains(body, "/api/tools/dns-lookup") || !strings.Contains(body, "/api/tools/ping") || !strings.Contains(body, "/api/apply/plan") || !strings.Contains(body, "/api/apply") || !strings.Contains(body, "Apply staged files") || !strings.Contains(body, "Apply live configs") || !strings.Contains(body, "Reload and health check services") || !strings.Contains(body, "Load apply history") || !strings.Contains(body, "Service status") || !strings.Contains(body, "loadServiceStatus") || !strings.Contains(body, "Client links") || !strings.Contains(body, "/api/client-links") || !strings.Contains(body, "/api/client-links/subscription") || !strings.Contains(body, "format=base64") || !strings.Contains(body, "format=raw") || !strings.Contains(body, "copy-client-links") || !strings.Contains(body, "copyClientLinksOutput") || !strings.Contains(body, "navigator.clipboard.writeText") || !strings.Contains(body, "download-client-subscription") || !strings.Contains(body, "download-client-subscription-raw") || !strings.Contains(body, "downloadClientSubscriptionPath") || !strings.Contains(body, "URL.createObjectURL") || !strings.Contains(body, "veil-subscription-raw.txt") {
					t.Fatalf("unexpected panel body: %s", body)
				}
			}
		})
	}
}

func TestRouterServesPanelShellWithApplyHistoryFilters(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		"apply-history-stage",
		"apply-history-success",
		"apply-history-limit",
		"loadApplyHistory",
		"/api/apply/history?",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing apply history filter control %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithSpeedtestControl(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Speedtest") || !strings.Contains(body, "/api/tools/speedtest") {
		t.Fatalf("expected speedtest control in panel shell: %s", body)
	}
}

func TestRouterServesPanelShellWithManagementSections(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"Inbounds", "Routing rules", "WARP", "/api/inbounds", "/api/routing/rules", "/api/warp"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithTokenControls(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
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

func TestRouterServesPanelShellWithManagementForms(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		"inbound-form",
		"inbound-name",
		"inbound-protocol",
		"inbound-transport",
		"saveInbound",
		"deleteInbound",
		"routing-rule-form",
		"routing-rule-name",
		"routing-rule-match",
		"routing-rule-outbound",
		"routing-preset-profile",
		"applyRoutingPreset",
		"/api/routing/presets",
		"saveRoutingRule",
		"deleteRoutingRule",
		"warp-form",
		"warp-enabled",
		"warp-private-key",
		"warp-local-address",
		"warp-peer-public-key",
		"saveWarpConfig",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing management form control %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithUpdateControls(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "update-version") || !strings.Contains(body, "Update panel") {
		t.Fatalf("expected update control in panel shell: %s", body)
	}
}

func TestRouterVersionUpdateEndpointRejectsGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/version/update", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET on update endpoint, got %d", w.Code)
	}
}

func TestRouterVersionUpdateEndpointRequiresAuth(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/version/update", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", w.Code)
	}
}
