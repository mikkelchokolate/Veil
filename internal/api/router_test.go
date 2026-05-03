package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterHealthz(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/healthz", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method == http.MethodGet && w.Body.String() != "ok\n" {
				t.Fatalf("unexpected body: %q", w.Body.String())
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Fatalf("unexpected healthz content-type: %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("expected no-store cache-control for healthz, got %q", cc)
			}
			if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
				t.Fatalf("expected no-cache pragma for healthz, got %q", pragma)
			}
			if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Fatalf("expected nosniff for healthz, got %q", nosniff)
			}
		})
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

func TestAuthErrorResponseIncludesSecurityHeaders(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on auth error, got %q", nosniff)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on auth error, got %q", cc)
	}
	if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("expected no-cache Pragma on auth error, got %q", pragma)
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

func TestRouterAcceptsBearerAuthTokenCaseInsensitive(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "bearer secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for lowercase bearer, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClientLinksEndpointBuildsEnabledProxyLinks(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ClientLinksResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode client links: %v", err)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing client links, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing client links, got %q", nosniff)
	}
	if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("expected no-cache Pragma for secret-bearing client links, got %q", pragma)
	}
	if response.Domain != "vpn.example.com" || response.Stack != "both" || response.SubscriptionURL != "/api/client-links/subscription" || response.Base64SubscriptionURL != "/api/client-links/subscription?format=base64" || response.RawSubscriptionURL != "/api/client-links/subscription?format=raw" || response.Count != 2 {
		t.Fatalf("unexpected client link metadata: %+v", response)
	}
	if response.SchemaVersion != "v1" {
		t.Fatalf("unexpected client links schema version: %q", response.SchemaVersion)
	}
	if response.DefaultSubscriptionFormat != "base64" {
		t.Fatalf("unexpected default subscription format: %q", response.DefaultSubscriptionFormat)
	}
	if response.Base64SubscriptionFilename != "veil-subscription.txt" || response.RawSubscriptionFilename != "veil-subscription-raw.txt" {
		t.Fatalf("unexpected subscription filenames: base64=%q raw=%q", response.Base64SubscriptionFilename, response.RawSubscriptionFilename)
	}
	if response.SubscriptionContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected subscription content type: %q", response.SubscriptionContentType)
	}
	if got := strings.Join(response.SubscriptionFormats, ","); got != "base64,raw" {
		t.Fatalf("unexpected subscription formats: %q", got)
	}
	if len(response.Links) != 2 {
		t.Fatalf("expected 2 client links, got %+v", response.Links)
	}
	links := map[string]ClientLink{}
	for _, link := range response.Links {
		links[link.Name] = link
	}
	if links["naive"].Protocol != "naiveproxy" || links["naive"].Transport != "tcp" || links["naive"].Port != 443 || links["naive"].URI != "https://veil:naive-secret@vpn.example.com:443" {
		t.Fatalf("unexpected naive link: %+v", links["naive"])
	}
	if links["hysteria2"].Protocol != "hysteria2" || links["hysteria2"].Transport != "udp" || links["hysteria2"].Port != 443 || !strings.HasPrefix(links["hysteria2"].URI, "hysteria2://hy2-secret@vpn.example.com:443/") || !strings.Contains(links["hysteria2"].URI, "sni=vpn.example.com") {
		t.Fatalf("unexpected hysteria2 link: %+v", links["hysteria2"])
	}
}

func TestClientLinksEndpointRequiresDomainAndPasswords(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for incomplete client links config, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClientLinksSubscriptionEndpointReturnsBase64URIs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing subscription, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing subscription, got %q", nosniff)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="veil-subscription.txt"` {
		t.Fatalf("unexpected content-disposition: %q", cd)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(w.Body.String()))
	if err != nil {
		t.Fatalf("decode subscription: %v; body=%q", err, w.Body.String())
	}
	assertClientSubscriptionLines(t, string(decoded))
}

func TestClientLinksSubscriptionEndpointReturnsRawURIs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=raw", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing raw subscription, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing raw subscription, got %q", nosniff)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="veil-subscription-raw.txt"` {
		t.Fatalf("unexpected raw content-disposition: %q", cd)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(w.Body.String())); err == nil {
		t.Fatalf("raw subscription should not be base64 encoded: %q", w.Body.String())
	}
	assertClientSubscriptionLines(t, w.Body.String())
}

func TestClientLinksSubscriptionEndpointRejectsUnknownFormat(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=json", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assertInvalidSubscriptionFormat(t, w)
}

func TestClientLinksSubscriptionEndpointRejectsUnknownFormatBeforeConfigValidation(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=json", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assertInvalidSubscriptionFormat(t, w)
}

func TestClientLinksSubscriptionEndpointRejectsUnknownQueryBeforeConfigValidation(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?offset=1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported subscription query, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `unsupported subscription query "offset"`) {
		t.Fatalf("unexpected unsupported query error: %q", w.Body.String())
	}
}

func assertInvalidSubscriptionFormat(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid subscription format, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "format must be base64 or raw") {
		t.Fatalf("unexpected invalid format error: %q", w.Body.String())
	}
}

func assertClientSubscriptionLines(t *testing.T, body string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 subscription links, got %q", body)
	}
	if lines[0] != "https://veil:naive-secret@vpn.example.com:443" {
		t.Fatalf("unexpected first subscription link: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "hysteria2://hy2-secret@vpn.example.com:443/") || !strings.Contains(lines[1], "sni=vpn.example.com") {
		t.Fatalf("unexpected second subscription link: %q", lines[1])
	}
}

func TestStatusEndpointIncludesRuntimeServiceStates(t *testing.T) {
	old := serviceStatusReader
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		switch unit {
		case "veil.service":
			return ServiceRuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
		case "caddy.service":
			return ServiceRuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "inactive", SubState: "dead"}
		default:
			return ServiceRuntimeStatus{Unit: unit, LoadState: "not-found", ActiveState: "unknown", SubState: "unknown", Error: "unit not found"}
		}
	}
	t.Cleanup(func() { serviceStatusReader = old })

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for JSON API response, got %q", nosniff)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for JSON API response, got %q", cc)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON API content-type with charset, got %q", ct)
	}
	var response StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if response.SchemaVersion != "v1" {
		t.Fatalf("unexpected status schema version: %q", response.SchemaVersion)
	}
	services := map[string]ServiceStatus{}
	for _, service := range response.Services {
		services[service.Name] = service
	}
	if services["veil"].Unit != "veil.service" || services["veil"].ActiveState != "active" || services["veil"].SubState != "running" {
		t.Fatalf("veil runtime status not included: %+v", services["veil"])
	}
	if services["naive"].Unit != "caddy.service" || services["naive"].ActiveState != "inactive" || services["naive"].SubState != "dead" {
		t.Fatalf("naive/caddy runtime status not included: %+v", services["naive"])
	}
	if services["hysteria2"].Unit != "hysteria2.service" || services["hysteria2"].ActiveState != "unknown" || services["hysteria2"].Error == "" {
		t.Fatalf("hysteria2 runtime status error not included: %+v", services["hysteria2"])
	}
}

func TestStatusEndpointSupportsHEAD(t *testing.T) {
	old := serviceStatusReader
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
	}
	t.Cleanup(func() { serviceStatusReader = old })

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	// HEAD /api/status returns 200, JSON/security headers, empty body
	t.Run("HEAD", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("expected empty HEAD body, got %d bytes: %q", w.Body.Len(), w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("expected JSON content-type with charset, got %q", ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("expected no-store cache-control, got %q", cc)
		}
		if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
			t.Fatalf("expected nosniff, got %q", nosniff)
		}
	})

	// unsupported method returns 405 with Allow: GET, HEAD
	t.Run("unsupported method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
		allow := w.Header().Get("Allow")
		if allow != "GET, HEAD" {
			t.Fatalf("expected Allow: GET, HEAD, got %q", allow)
		}
	})
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
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r := NewRouter(ServerInfo{Version: "test"})
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
			if csp := w.Header().Get("Content-Security-Policy"); csp != "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'" {
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
				if body := w.Body.String(); !strings.Contains(body, "Veil Panel") || !strings.Contains(body, "/api/status") || !strings.Contains(body, "/api/apply/plan") || !strings.Contains(body, "/api/apply") || !strings.Contains(body, "Apply staged files") || !strings.Contains(body, "Apply live configs") || !strings.Contains(body, "Reload and health check services") || !strings.Contains(body, "Load apply history") || !strings.Contains(body, "Service status") || !strings.Contains(body, "loadServiceStatus") || !strings.Contains(body, "Client links") || !strings.Contains(body, "/api/client-links") || !strings.Contains(body, "/api/client-links/subscription") || !strings.Contains(body, "format=base64") || !strings.Contains(body, "format=raw") || !strings.Contains(body, "copy-client-links") || !strings.Contains(body, "copyClientLinksOutput") || !strings.Contains(body, "navigator.clipboard.writeText") || !strings.Contains(body, "download-client-subscription") || !strings.Contains(body, "download-client-subscription-raw") || !strings.Contains(body, "downloadClientSubscriptionPath") || !strings.Contains(body, "URL.createObjectURL") || !strings.Contains(body, "veil-subscription-raw.txt") {
					t.Fatalf("unexpected panel body: %s", body)
				}
			}
		})
	}
}

func TestRouterServesPanelShellWithApplyHistoryFilters(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
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

func TestRURecommendedPreviewRejectsOversizedJSONBody(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"` + strings.Repeat("a", 1024*1024+1) + `","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized preview body, got %d: %s", w.Code, w.Body.String())
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

func TestRouterServesPanelShellWithManagementForms(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		"settings-form",
		"settings-domain",
		"settings-naive-password",
		"saveSettings",
		"loadSettingsIntoForm",
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"enabled":true,"endpoint":"engage.cloudflareclient.com:2408","privateKey":"` + strings.Repeat("a", 1024*1024+1) + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized WARP body, got %d with response length %d", w.Code, w.Body.Len())
	}
}

func TestManagementAPIWarpPutRejectsUnknownJSONFields(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","domain":"` + strings.Repeat("a", 1024*1024+1) + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized settings body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPISettingsResponsesRedactSecrets(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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

func TestManagementAPIUpdatesAndDeletesInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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

	restarted := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/hysteria2", strings.NewReader(`{"protocol":"hysteria2","transport":"tcp","port":443,"enabled":true}`)))
	if update.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port on update, got %d: %s", update.Code, update.Body.String())
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

func TestManagementAPIRoutingRulesRejectOversizedJSONBodies(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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

	restarted := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/rules/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing routing rule, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIExposesRoutingPresetProfiles(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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

	restarted := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	read := httptest.NewRecorder()
	restarted.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(read.Body.String(), "preset-all-except-russia") || !strings.Contains(read.Body.String(), "geosite:category-ru") {
		t.Fatalf("persisted preset routing rules missing: %s", read.Body.String())
	}
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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

func TestManagementAPIRejectsUnknownRoutingPreset(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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

func testSHA256Line(body string, name string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func TestManagementApplyPlanValidatesAndReturnsStagedActions(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: t.TempDir()})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("apply %d expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	freshRouter := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})

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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(applyRoot, "generated", "veil", "apply-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid apply should not write files, stat err: %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func writeRenderableManagementState(path string, stack string) error {
	return os.WriteFile(path, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"stack":"`+stack+`",
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
	}`), 0o600)
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
	if len(body.Services) != 4 {
		t.Fatalf("expected 4 services, got %+v", body.Services)
	}
}

func TestManagementErrorResponsesIncludeSecurityHeaders(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"","mode":""}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required settings fields, got %d: %s", w.Code, w.Body.String())
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on management error, got %q", nosniff)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on management error, got %q", cc)
	}
}

func TestMethodNotAllowedResponsesIncludeAllowAndSecurityHeaders(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for unsupported method, got %d: %s", w.Code, w.Body.String())
	}
	if allow := w.Header().Get("Allow"); allow != "GET, PUT" {
		t.Fatalf("expected Allow header to list supported settings methods, got %q", allow)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on method-not-allowed error, got %q", nosniff)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on method-not-allowed error, got %q", cc)
	}
}

func TestJSONDecodeErrorResponseIncludesSecurityHeaders(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d: %s", w.Code, w.Body.String())
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on JSON decode error, got %q", nosniff)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on JSON decode error, got %q", cc)
	}
}

func TestIsAllowedHealthService(t *testing.T) {
	tests := []struct {
		service string
		want    bool
	}{
		{"veil-naive.service", true},
		{"veil-hysteria2.service", true},
		{"veil-warp.service", true},
		{"veil.service", false},
		{"caddy.service", false},
		{"hysteria2.service", false},
		{"", false},
		{"veil-naive", false},
		{"veil-naive.service.evil", false},
	}
	for _, tt := range tests {
		got := isAllowedHealthService(tt.service)
		if got != tt.want {
			t.Errorf("isAllowedHealthService(%q) = %v, want %v", tt.service, got, tt.want)
		}
	}
}

func TestIsAllowedServiceCommand(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    bool
	}{
		{
			name:    "reload naive",
			command: []string{"systemctl", "reload", "veil-naive.service"},
			want:    true,
		},
		{
			name:    "reload hysteria2",
			command: []string{"systemctl", "reload", "veil-hysteria2.service"},
			want:    true,
		},
		{
			name:    "reload warp",
			command: []string{"systemctl", "reload", "veil-warp.service"},
			want:    true,
		},
		{
			name:    "non-systemctl",
			command: []string{"bash", "reload", "veil-naive.service"},
			want:    false,
		},
		{
			name:    "non-reload",
			command: []string{"systemctl", "restart", "veil-naive.service"},
			want:    false,
		},
		{
			name:    "too few args",
			command: []string{"systemctl", "reload"},
			want:    false,
		},
		{
			name:    "too many args",
			command: []string{"systemctl", "reload", "veil-naive.service", "extra"},
			want:    false,
		},
		{
			name:    "unlisted service",
			command: []string{"systemctl", "reload", "caddy.service"},
			want:    false,
		},
		{
			name:    "empty command",
			command: []string{},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAllowedServiceCommand(tt.command)
			if got != tt.want {
				t.Errorf("isAllowedServiceCommand(%v) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestManagementAPIGetsInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

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
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing inbound, got %d: %s", w.Code, w.Body.String())
	}
}
