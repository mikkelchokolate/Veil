package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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
			name:    "reload naive",
			command: []string{"systemctl", "reload", "veil-caddy@panel.service"},
			want:    true,
		},
		{
			name:    "reload hysteria2 template",
			command: []string{"systemctl", "reload", "veil-hysteria2@.service"},
			want:    true,
		},
		{
			name:    "reload hysteria2 instance",
			command: []string{"systemctl", "reload", "veil-hysteria2@default.service"},
			want:    true,
		},
		{
			name:    "reload warp",
			command: []string{"systemctl", "reload", "veil-warp.service"},
			want:    true,
		},
		{
			name:    "non-systemctl",
			command: []string{"bash", "reload", "veil-caddy@panel.service"},
			want:    false,
		},
		{
			name:    "non-reload",
			command: []string{"systemctl", "restart", "veil-caddy@panel.service"},
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

func TestBearerTokenReturnsEmptyForHeaderExactlySchemeLengthWithNoToken(t *testing.T) {
	if got := bearerToken("Bearer "); got != "" {
		t.Fatalf("expected empty for header exactly 'Bearer ' with no token, got %q", got)
	}
}

func TestBearerTokenReturnsEmptyForNonBearerScheme(t *testing.T) {
	tests := []string{
		"Basic abcdefghij",
		"Digest abcdefghij",
		"bearer", // no space, shorter than scheme
		"BEARERTOKEN",
		"NotBearer xyz",
	}
	for _, h := range tests {
		t.Run("header="+h, func(t *testing.T) {
			if got := bearerToken(h); got != "" {
				t.Fatalf("expected empty for non-Bearer header %q, got %q", h, got)
			}
		})
	}
}

func TestBearerTokenExtractsTokenCaseInsensitively(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer token", "token"},
		{"bearer token", "token"},
		{"BEARER token", "token"},
		{"BeArEr token", "token"},
	}
	for _, tt := range tests {
		t.Run("header="+tt.header, func(t *testing.T) {
			if got := bearerToken(tt.header); got != tt.want {
				t.Fatalf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestBearerTokenTrimsWhitespaceFromToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer   token  ", "token"},
		{"Bearer \t token \t", "token"},
		{"Bearer token", "token"},
		{"Bearer  multi word token ", "multi word token"},
	}
	for _, tt := range tests {
		t.Run("header="+tt.header, func(t *testing.T) {
			if got := bearerToken(tt.header); got != tt.want {
				t.Fatalf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestDecodeJSONRequestRejectsNonJSONContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantStatus  int
		wantOK      bool
	}{
		{"application/json", "application/json", http.StatusOK, true},
		{"no content type", "", http.StatusOK, true},
		{"text/plain", "text/plain", http.StatusUnsupportedMediaType, false},
		{"application/xml", "application/xml", http.StatusUnsupportedMediaType, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(`{"domain":"example.com"}`)
			req := httptest.NewRequest("POST", "/api/settings", body)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			var v struct {
				Domain string `json:"domain"`
			}
			got := decodeJSONRequest(rec, req, &v)
			if got != tt.wantOK {
				t.Fatalf("decodeJSONRequest() = %v, want %v", got, tt.wantOK)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestWriteJSON_LogsEncodeError(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	w := httptest.NewRecorder()
	writeJSON(w, make(chan int))

	if !strings.Contains(buf.String(), "writeJSON: encode error") {
		t.Errorf("expected log message containing 'writeJSON: encode error', got: %s", buf.String())
	}
}

func TestWriteJSONStatus_LogsEncodeError(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	w := httptest.NewRecorder()
	writeJSONStatus(w, http.StatusOK, make(chan int))

	if !strings.Contains(buf.String(), "writeJSONStatus: encode error") {
		t.Errorf("expected log message containing 'writeJSONStatus: encode error', got: %s", buf.String())
	}
}

func TestWriteJSON_NoError_NoLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"hello": "world"})

	if buf.Len() != 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

func TestWriteJSONStatus_NoError_NoLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	w := httptest.NewRecorder()
	writeJSONStatus(w, http.StatusOK, map[string]string{"hello": "world"})

	if buf.Len() != 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

func TestValidateEmptyJSONBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     bool
	}{
		{"no content type, no body", "", "", false},
		{"no content type, empty object", "", "{}", false},
		{"json content type, no body", "application/json", "", false},
		{"json content type, empty object", "application/json", "{}", false},
		{"json content type, whitespace", "application/json", "  {}\n  ", false},
		{"wrong content type", "text/plain", "", true},
		{"json content type, unexpected body", "application/json", `"hello"`, true},
		{"json content type, array body", "application/json", `[1,2,3]`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			err := validateEmptyJSONBody(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmptyJSONBody() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	paths := []string{"/", "/healthz", "/api/status", "/api/settings", "/api/warp", "/api/nonexistent"}

	for _, path := range paths {
		t.Run("path="+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options: want nosniff, got %q", got)
			}
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options: want DENY, got %q", got)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy: want no-referrer, got %q", got)
			}
			if got := w.Header().Get("X-Permitted-Cross-Domain-Policies"); got != "none" {
				t.Errorf("X-Permitted-Cross-Domain-Policies: want none, got %q", got)
			}
			if got := w.Header().Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
				t.Errorf("Cross-Origin-Resource-Policy: want same-origin, got %q", got)
			}
			if got := w.Header().Get("X-DNS-Prefetch-Control"); got != "off" {
				t.Errorf("X-DNS-Prefetch-Control: want off, got %q", got)
			}
			if got := w.Header().Get("Server"); got != "" {
				t.Errorf("Server: want empty (hidden), got %q", got)
			}
			// HSTS only on HTTPS
			if got := w.Header().Get("Strict-Transport-Security"); got != "" {
				t.Errorf("Strict-Transport-Security: want empty on HTTP, got %q", got)
			}
		})
	}
}

func TestSecurityHeadersMiddlewareOnErrorPaths(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", AuthToken: "secret"})

	// Unauthorized POST without token — should still have security headers
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options on unauthorized response")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options on unauthorized response")
	}
}

func TestSecurityHeadersMiddlewareHSTSOnHTTPS(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// Simulate a TLS connection by setting req.TLS
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	hsts := w.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("expected Strict-Transport-Security header on HTTPS request")
	}
	if !strings.Contains(hsts, "max-age=63072000") {
		t.Errorf("expected HSTS max-age in %q", hsts)
	}
	if !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("expected includeSubDomains in %q", hsts)
	}
}

func TestLogsEndpointRequiresGET(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestLogsEndpointRejectsInvalidUnit(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/logs?unit=rm%20-rf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid unit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogsEndpointRejectsInvalidLines(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	for _, qs := range []string{"lines=0", "lines=501", "lines=abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/logs?"+qs, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %s, got %d: %s", qs, w.Code, w.Body.String())
		}
	}
}

func TestValidLogUnit(t *testing.T) {
	tests := []struct {
		unit string
		want bool
	}{
		{"veil", true},
		{"caddy", true},
		{"hysteria2", true},
		{"sing-box", true},
		{"veil-caddy@panel", true},
		{"my_service@1", true},
		{"foo.bar", true},
		{"", false},
		{"rm -rf", false},
		{"foo;bar", false},
		{"cat /etc/passwd", false},
		{"$(whoami)", false},
		{"foo`id`", false},
	}
	for _, tt := range tests {
		t.Run("unit="+tt.unit, func(t *testing.T) {
			if got := validLogUnit(tt.unit); got != tt.want {
				t.Errorf("validLogUnit(%q) = %v, want %v", tt.unit, got, tt.want)
			}
		})
	}
}
