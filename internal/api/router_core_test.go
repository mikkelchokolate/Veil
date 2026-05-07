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

var _routerTestDeps_router_core_test = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestRouterHealthz(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/healthz", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method == http.MethodGet {
				var body map[string]string
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode healthz body: %v", err)
				}
				if body["status"] != "ok" {
					t.Fatalf("unexpected body: %+v", body)
				}
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
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

func TestHealthzReturnsOKWhenStateAccessible(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "management-state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when state accessible, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode healthz body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %+v", body)
	}
}

func TestHealthzReturns503WhenStateMissing(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "nonexistent", "management-state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when state missing, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode healthz body: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status unhealthy, got %+v", body)
	}
	if body["error"] == "" {
		t.Fatalf("expected error message in unhealthy response, got %+v", body)
	}
}

func TestRouterRequiresAuthTokenForAPIWhenConfigured(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
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
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
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
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterAcceptsBearerAuthTokenCaseInsensitive(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "bearer secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for lowercase bearer, got %d: %s", w.Code, w.Body.String())
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

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

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
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
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
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected public healthz 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterStatus(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	if len(body.Services) != 5 {
		t.Fatalf("expected 5 services, got %+v", body.Services)
	}
}

func TestAPIVersionEndpoint(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "1.2.3"})

	t.Run("GET returns version JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "application/json; charset=utf-8" {
			t.Fatalf("expected json content-type, got %q", ct)
		}
		var body map[string]string
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["version"] != "1.2.3" {
			t.Errorf("version: want 1.2.3, got %q", body["version"])
		}
		if body["name"] != "Veil" {
			t.Errorf("name: want Veil, got %q", body["name"])
		}
		if body["runtime"] == "" {
			t.Error("runtime must not be empty")
		}
	})

	t.Run("HEAD returns headers with empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/version", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
		}
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}
