package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestManagementErrorResponsesIncludeSecurityHeaders(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":""}`)
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
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
		{"veil-caddy@panel.service", true},
		{"veil-hysteria2@default.service", true},
		{"veil-hysteria2@.service", true},
		{"veil-warp.service", true},
		{"veil.service", false},
		{"caddy.service", false},
		{"hysteria2.service", false},
		{"", false},
		{"veil-caddy@panel", false},
		{"veil-caddy@panel.service.evil", false},
	}
	for _, tt := range tests {
		got := service.NewCommandPolicy(NewManagedRuntimeCatalog()).AllowsHealth(tt.service)
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
			name:    "restart naive",
			command: []string{"systemctl", "restart", "veil-caddy@panel.service"},
			want:    true,
		},
		{
			name:    "restart hysteria2 template",
			command: []string{"systemctl", "restart", "veil-hysteria2@.service"},
			want:    true,
		},
		{
			name:    "restart hysteria2 instance",
			command: []string{"systemctl", "restart", "veil-hysteria2@default.service"},
			want:    true,
		},
		{
			name:    "restart warp",
			command: []string{"systemctl", "restart", "veil-warp.service"},
			want:    true,
		},
		{
			name:    "non-systemctl",
			command: []string{"bash", "reload", "veil-caddy@panel.service"},
			want:    false,
		},
		{
			name:    "non-allowed verb",
			command: []string{"systemctl", "status", "veil-caddy@panel.service"},
			want:    false,
		},
		{
			name:    "too few args",
			command: []string{"systemctl", "reload"},
			want:    false,
		},
		{
			name:    "too many args",
			command: []string{"systemctl", "reload", "veil-caddy@panel.service", "extra"},
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
			got := service.NewCommandPolicy(NewManagedRuntimeCatalog()).AllowsAction(tt.command)
			if got != tt.want {
				t.Errorf("isAllowedServiceCommand(%v) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestLogRoutesResolvePerInboundRuntimeUnits(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	routes := LogRoutes{State: state}

	if unit, ok := routes.resolveLogUnit("hysteria2-edge"); !ok || unit != "veil-hysteria2@edge.service" {
		t.Fatalf("resolve action name = %q %v", unit, ok)
	}
	if unit, ok := routes.resolveLogUnit("veil-hysteria2@edge.service"); !ok || unit != "veil-hysteria2@edge.service" {
		t.Fatalf("resolve unit = %q %v", unit, ok)
	}
}

func TestLogRoutesRejectUnknownRuntimeUnits(t *testing.T) {
	routes := LogRoutes{State: newManagementState(ServerInfo{Version: "test", Mode: "dev"})}
	if unit, ok := routes.resolveLogUnit("caddy.service"); ok || unit != "" {
		t.Fatalf("unexpected unit resolution: %q %v", unit, ok)
	}
}

func TestReadSystemdServiceStatusTimeout(t *testing.T) {
	old := serviceStatusReader
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{
			Unit:        unit,
			LoadState:   "unknown",
			ActiveState: "unknown",
			SubState:    "unknown",
			Error:       "context deadline exceeded",
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

	var response StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}

	// Verify timeout errors are surfaced in service status
	for _, svc := range response.Services {
		if svc.Error == "" {
			t.Fatalf("expected timeout error in service %s status, got none", svc.Name)
		}
		if !strings.Contains(svc.Error, "deadline") {
			t.Fatalf("expected deadline error in service %s, got: %s", svc.Name, svc.Error)
		}
	}
}

func TestBearerTokenReturnsEmptyForEmptyHeader(t *testing.T) {
	if got := bearerToken(""); got != "" {
		t.Fatalf("expected empty string for empty header, got %q", got)
	}
}

func TestBearerTokenReturnsEmptyForHeaderShorterThanScheme(t *testing.T) {
	tests := []string{"B", "Be", "Bea", "Bear", "Beare", "Bearer"}
	for _, h := range tests {
		t.Run("header="+h, func(t *testing.T) {
			if got := bearerToken(h); got != "" {
				t.Fatalf("expected empty for header %q (shorter than scheme), got %q", h, got)
			}
		})
	}
}

func TestBearerTokenReturnsToken(t *testing.T) {
	if got := bearerToken("Bearer abc.def"); got != "abc.def" {
		t.Fatalf("token=%q", got)
	}
}

func TestWithSecurityHeadersHidesServerHeader(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	handler := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "go-test")
		w.WriteHeader(http.StatusOK)
	}), logger)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if server := w.Header().Get("Server"); server != "" {
		t.Fatalf("expected Server header stripped, got %q", server)
	}
}

func TestTLSConfigurationEnforcesModernMinimum(t *testing.T) {
	r, server := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	httpServer := &http.Server{Handler: r}
	ApplyServerTLSConfig(httpServer, SecurityConfig{EnableHSTS: true})
	if httpServer.TLSConfig == nil {
		t.Fatal("expected TLS config")
	}
	if httpServer.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("min TLS version = %x", httpServer.TLSConfig.MinVersion)
	}
	if httpServer.TLSConfig.ClientSessionCache == nil {
		t.Fatal("expected client session cache")
	}
	server.Close()
}

func TestAccessLogMiddlewareWritesMethodPathAndStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	handler := accessLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), logger)
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	line := buf.String()
	if !strings.Contains(line, "POST") || !strings.Contains(line, "/api/test") || !strings.Contains(line, "201") {
		t.Fatalf("unexpected access log line: %q", line)
	}
}

func TestCORSMiddlewareAddsHeadersForAllowedOrigin(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if allow := w.Header().Get("Access-Control-Allow-Origin"); allow != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin=%q", allow)
	}
	if vary := w.Header().Get("Vary"); vary != "Origin" {
		t.Fatalf("Vary=%q", vary)
	}
}

func TestCORSMiddlewareSkipsWhenNoOrigin(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header without origin, got %q", got)
	}
}

func TestNoCacheAPIMiddlewareSetsNoStoreForAPI(t *testing.T) {
	handler := noCacheAPIMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func TestNoCacheAPIMiddlewareDoesNotOverrideStaticCache(t *testing.T) {
	handler := noCacheAPIMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("expected static cache header preserved, got %q", got)
	}
}
