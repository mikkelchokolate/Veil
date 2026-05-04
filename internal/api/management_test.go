package api

import (
	"bytes"
	"crypto/rand"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veil-panel/veil/internal/secrets"
)

func TestDownloadRouteDatReturnsBodyOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("route-dat-content"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "route-dat-content" {
		t.Fatalf("expected route-dat-content, got %q", string(body))
	}
}

func TestDownloadRouteDatReturnsErrorOnNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestDownloadRouteDatReturnsErrorOnInvalidURL(t *testing.T) {
	_, err := downloadRouteDat("://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// retryTestTransport is a http.RoundTripper that fails the first failCount
// requests with a connection error, then delegates to the base transport.
type retryTestTransport struct {
	attempts  *int
	failCount int
	base      http.RoundTripper
}

func (t *retryTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*t.attempts++
	if *t.attempts <= t.failCount {
		return nil, &testNetworkError{msg: "simulated connection refused"}
	}
	return t.base.RoundTrip(req)
}

type testNetworkError struct{ msg string }

func (e *testNetworkError) Error() string   { return e.msg }
func (e *testNetworkError) Timeout() bool   { return false }
func (e *testNetworkError) Temporary() bool { return true }

func TestDownloadRouteDatRetriesOnServerError(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success-after-retries"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if string(body) != "success-after-retries" {
		t.Fatalf("expected success-after-retries, got %q", string(body))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDownloadRouteDatRetriesOnConnectionRefused(t *testing.T) {
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success-after-conn-retries"))
	}))
	defer server.Close()

	routeDatHTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &retryTestTransport{
			attempts:  &attempts,
			failCount: 2,
			base:      server.Client().Transport,
		},
	}

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "success-after-conn-retries" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDownloadRouteDatGivesUpAfterMaxRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts before giving up, got %d", attempts)
	}
}

func TestDownloadRouteDatNoRetryOn4xx(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestDownloadRouteDatLogsRetries(t *testing.T) {
	var buf bytes.Buffer
	oldLogger := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldLogger) })

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "retry") && !strings.Contains(logOutput, "Retry") && !strings.Contains(logOutput, "attempt") {
		t.Fatalf("expected retry message in log output, got: %s", logOutput)
	}
}

func TestStackAllowsProtocolRejectsCrossStackProtocols(t *testing.T) {
	tests := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"naive", "naiveproxy", true},
		{"naive", "hysteria2", false},
		{"hysteria2", "hysteria2", true},
		{"hysteria2", "naiveproxy", false},
		{"both", "naiveproxy", true},
		{"both", "hysteria2", true},
		{"unknown", "naiveproxy", true},
		{"unknown", "hysteria2", true},
	}
	for _, tt := range tests {
		t.Run(tt.stack+"/"+tt.protocol, func(t *testing.T) {
			got := stackAllowsProtocol(tt.stack, tt.protocol)
			if got != tt.want {
				t.Fatalf("stackAllowsProtocol(%q, %q) = %v, want %v", tt.stack, tt.protocol, got, tt.want)
			}
		})
	}
}

func TestRunFixedServiceActionRejectsDisallowedCommands(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		wantErr string
	}{
		{"wrong binary", []string{"bash", "reload", "veil-naive.service"}, "service command is not allowed"},
		{"wrong subcommand", []string{"systemctl", "restart", "veil-naive.service"}, "service command is not allowed"},
		{"wrong service", []string{"systemctl", "reload", "evil.service"}, "service command is not allowed"},
		{"too few args", []string{"systemctl", "reload"}, "service command is not allowed"},
		{"too many args", []string{"systemctl", "reload", "veil-naive.service", "extra"}, "service command is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runFixedServiceAction(tt.command)
			if result.Success {
				t.Fatal("expected failure for disallowed command")
			}
			if result.Error != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, result.Error)
			}
			if result.Name != tt.command[len(tt.command)-1] {
				t.Fatalf("expected name from last arg, got %q", result.Name)
			}
		})
	}
}

func TestRunFixedServiceHealthCheckRejectsDisallowedServices(t *testing.T) {
	tests := []struct {
		name    string
		service string
		wantErr string
	}{
		{"unknown service", "unknown.service", "service health check is not allowed"},
		{"nginx service", "nginx.service", "service health check is not allowed"},
		{"empty service", "", "service health check is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runFixedServiceHealthCheck(tt.service)
			if result.Healthy {
				t.Fatal("expected not healthy for disallowed service")
			}
			if result.Error != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, result.Error)
			}
			if result.Name != tt.service {
				t.Fatalf("expected name %q, got %q", tt.service, result.Name)
			}
			expectedCommand := []string{"systemctl", "is-active", "--quiet", tt.service}
			if len(result.Command) != len(expectedCommand) {
				t.Fatalf("expected command %v, got %v", expectedCommand, result.Command)
			}
		})
	}
}

func TestVerifyRouteDatChecksumSuccessWithStandardFormat(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessWithSha256Prefix(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "sha256:3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error with sha256 prefix: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessMultipleEntries(t *testing.T) {
	geositeBody := []byte("different content for geosite")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n" +
		"8bfd86422903167e5f93020206abdfdd52a2ae3cdb76e3e4fbafa586a043a50a  geosite.dat\n"

	err := verifyRouteDatChecksum("geosite.dat", geositeBody, checksumText)
	if err != nil {
		t.Fatalf("unexpected error for second entry: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessFallbackToFirstToken(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6\n"

	err := verifyRouteDatChecksum("other-file.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error when falling back to first token: %v", err)
	}
}

func TestVerifyRouteDatChecksumMismatch(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected 'checksum mismatch' error, got: %v", err)
	}
}

func TestVerifyRouteDatChecksumEmptyText(t *testing.T) {
	body := []byte("content")
	err := verifyRouteDatChecksum("geoip.dat", body, "")
	if err == nil {
		t.Fatal("expected error for empty checksum text, got nil")
	}
	if !strings.Contains(err.Error(), "checksum for geoip.dat is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRouteDatChecksumWhitespaceOnly(t *testing.T) {
	body := []byte("content")
	err := verifyRouteDatChecksum("geoip.dat", body, "  \n\t  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only checksum, got nil")
	}
}

func TestVerifyRouteDatChecksumInvalidHex(t *testing.T) {
	body := []byte("content")
	checksumText := "not-a-valid-hex-string!!!  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected error for invalid hex checksum, got nil")
	}
	if !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("expected 'invalid checksum' error, got: %v", err)
	}
}

func TestVerifyRouteDatChecksumWrongLengthHex(t *testing.T) {
	body := []byte("content")
	// Only 8 hex chars instead of 64 (32 bytes)
	checksumText := "deadbeef  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected error for wrong-length hex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("expected 'invalid checksum' error, got: %v", err)
	}
}

func TestSetWarpDefaultsFillsAllMissingFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  WarpConfig
		want WarpConfig
	}{
		{
			name: "all empty",
			cfg:  WarpConfig{},
			want: WarpConfig{
				Endpoint:    "engage.cloudflareclient.com:2408",
				SocksListen: "127.0.0.1",
				SocksPort:   40000,
				MTU:         1280,
			},
		},
		{
			name: "endpoint empty only",
			cfg:  WarpConfig{SocksListen: "10.0.0.1", SocksPort: 9999, MTU: 1500},
			want: WarpConfig{
				Endpoint:    "engage.cloudflareclient.com:2408",
				SocksListen: "10.0.0.1",
				SocksPort:   9999,
				MTU:         1500,
			},
		},
		{
			name: "preserves existing values",
			cfg:  WarpConfig{Endpoint: "custom:1234", SocksListen: "0.0.0.0", SocksPort: 8080, MTU: 9000},
			want: WarpConfig{Endpoint: "custom:1234", SocksListen: "0.0.0.0", SocksPort: 8080, MTU: 9000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWarpDefaults(&tt.cfg)
			if tt.cfg.Endpoint != tt.want.Endpoint || tt.cfg.SocksListen != tt.want.SocksListen || tt.cfg.SocksPort != tt.want.SocksPort || tt.cfg.MTU != tt.want.MTU {
				t.Fatalf("setWarpDefaults = %+v, want %+v", tt.cfg, tt.want)
			}
		})
	}
}

func TestRunFixedConfigValidationEmptyCommand(t *testing.T) {
	result := runFixedConfigValidation("test", "/path/to/config", nil)
	if !result.Skipped {
		t.Fatal("expected skipped for empty command")
	}
	if result.Error != "validator command is empty" {
		t.Fatalf("expected 'validator command is empty', got %q", result.Error)
	}
	if result.Name != "test" || result.Config != "/path/to/config" {
		t.Fatalf("expected name/config preserved, got %+v", result)
	}
	if result.Command != nil {
		t.Fatalf("expected command preserved as nil, got %v", result.Command)
	}
}

func TestRunFixedConfigValidationBinaryNotFound(t *testing.T) {
	result := runFixedConfigValidation("sing-box", "/etc/veil/generated/sing-box/warp.json", []string{"nonexistent-validator", "check", "-c", "/etc/veil/generated/sing-box/warp.json"})
	if !result.Skipped {
		t.Fatal("expected skipped when binary not found")
	}
	if result.Error != "nonexistent-validator not found; syntax validation skipped" {
		t.Fatalf("expected binary not found error, got %q", result.Error)
	}
	if result.Name != "sing-box" {
		t.Fatalf("expected name preserved, got %q", result.Name)
	}
	if len(result.Command) != 4 || result.Command[0] != "nonexistent-validator" {
		t.Fatalf("expected command preserved, got %v", result.Command)
	}
}

func TestApplyHistoryStageReturnsCorrectStage(t *testing.T) {
	tests := []struct {
		name     string
		response ApplyResponse
		want     string
	}{
		{
			name:     "rollback stage",
			response: ApplyResponse{RolledBack: true},
			want:     "rollback",
		},
		{
			name:     "services stage supersedes live",
			response: ApplyResponse{ServicesApplied: true, LiveApplied: true},
			want:     "services",
		},
		{
			name:     "live stage",
			response: ApplyResponse{LiveApplied: true},
			want:     "live",
		},
		{
			name:     "staged fallback",
			response: ApplyResponse{},
			want:     "staged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyHistoryStage(tt.response)
			if got != tt.want {
				t.Fatalf("applyHistoryStage() = %q, want %q", got, tt.want)
			}
		})
	}
}

type timeoutRecordingTransport struct {
	onRoundTrip func(req *http.Request)
}

func (t *timeoutRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.onRoundTrip(req)
	return http.DefaultTransport.RoundTrip(req)
}

func TestLivePathForStagedConfig(t *testing.T) {
	state := &managementState{
		applyRoot: "/tmp/veil-test",
	}

	tests := []struct {
		name       string
		stagedPath string
		wantPath   string
		wantOK     bool
	}{
		// Known live paths
		{
			name:       "caddy Caddyfile",
			stagedPath: "/tmp/veil-test/generated/caddy/Caddyfile",
			wantPath:   "/tmp/veil-test/live/caddy/Caddyfile",
			wantOK:     true,
		},
		{
			name:       "hysteria2 server.yaml",
			stagedPath: "/tmp/veil-test/generated/hysteria2/server.yaml",
			wantPath:   "/tmp/veil-test/live/hysteria2/server.yaml",
			wantOK:     true,
		},
		{
			name:       "sing-box warp.json",
			stagedPath: "/tmp/veil-test/generated/sing-box/warp.json",
			wantPath:   "/tmp/veil-test/live/sing-box/warp.json",
			wantOK:     true,
		},
		// Unknown generated paths (valid prefix but not a known config)
		{
			name:       "unknown generated file",
			stagedPath: "/tmp/veil-test/generated/unknown/config.yaml",
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "generated prefix with no trailing path",
			stagedPath: "/tmp/veil-test/generated/",
			wantPath:   "",
			wantOK:     false,
		},
		// Paths outside the apply root
		{
			name:       "completely different root",
			stagedPath: "/other/path/generated/caddy/Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "apply root as substring but not prefix",
			stagedPath: "/var/tmp/veil-test-extra/generated/caddy/Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
		// Paths without the generated prefix (under apply root but not in generated/)
		{
			name:       "staged directory instead of generated",
			stagedPath: "/tmp/veil-test/staged/caddy/Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "live directory instead of generated",
			stagedPath: "/tmp/veil-test/live/caddy/Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
		// Edge cases
		{
			name:       "empty path",
			stagedPath: "",
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "just generated prefix no root",
			stagedPath: "generated/caddy/Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOK := state.livePathForStagedConfig(tt.stagedPath)
			if gotPath != tt.wantPath {
				t.Fatalf("livePathForStagedConfig(%q) path = %q, want %q", tt.stagedPath, gotPath, tt.wantPath)
			}
			if gotOK != tt.wantOK {
				t.Fatalf("livePathForStagedConfig(%q) ok = %v, want %v", tt.stagedPath, gotOK, tt.wantOK)
			}
		})
	}
}

func TestLivePathForStagedConfigTrailingSlashRoot(t *testing.T) {
	// applyRoot with trailing slash: TrimRight in prefix calculation normalizes it
	state := &managementState{
		applyRoot: "/tmp/veil-test/",
	}

	gotPath, gotOK := state.livePathForStagedConfig("/tmp/veil-test/generated/caddy/Caddyfile")
	wantPath := "/tmp/veil-test/live/caddy/Caddyfile"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if !gotOK {
		t.Fatal("expected ok=true")
	}
}

func TestDownloadRouteDatUsesHttpClientWithTimeout(t *testing.T) {
	// Verify the default client has a finite timeout
	if routeDatHTTPClient.Timeout == 0 {
		t.Fatal("routeDatHTTPClient should have a non-zero timeout")
	}

	// Verify timeout is applied to requests by using a custom transport
	// that records whether the request context has a deadline
	var hasDeadline bool
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })

	routeDatHTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &timeoutRecordingTransport{
			onRoundTrip: func(req *http.Request) {
				_, ok := req.Context().Deadline()
				hasDeadline = ok
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasDeadline {
		t.Fatal("expected request context to have a deadline (timeout) but it did not")
	}
}

func TestDownloadRouteDatRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write content exceeding the 50 MB limit
		chunk := make([]byte, 1024*1024) // 1 MB chunks
		for i := 0; i < 51; i++ {        // 51 MB total > 50 MB limit
			_, _ = w.Write(chunk)
		}
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for oversized body (>50MB), got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") && !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected error to mention size limit, got: %v", err)
	}
}

func TestDownloadRouteDatWithinSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("small-body"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "small-body" {
		t.Fatalf("expected small-body, got %q", string(body))
	}
}

func TestManagementStateLoadReturnsErrorOnCorruptedState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "management-state.json")

	// Write corrupted (invalid JSON) state file
	if err := os.WriteFile(statePath, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	state := &managementState{statePath: statePath}
	err := state.load()
	if err == nil {
		t.Fatal("expected error for corrupted state file, got nil")
	}
	if !strings.Contains(err.Error(), "invalid character") && !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected error to mention invalid character or unmarshal, got: %v", err)
	}
}

func TestNewManagementStateLogsCorruptedStateError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "management-state.json")

	// Write corrupted (invalid JSON) state file
	if err := os.WriteFile(statePath, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Create management state via NewRouter (which calls newManagementState internally)
	_ = NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output when loading corrupted state file, but log was empty")
	}
	if !strings.Contains(output, "management-state.json") {
		t.Fatalf("expected log output to mention state file, got: %s", output)
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		value  string
		want   []string
	}{
		{
			name:  "append to nil slice",
			input: nil,
			value: "a",
			want:  []string{"a"},
		},
		{
			name:  "append to empty slice",
			input: []string{},
			value: "b",
			want:  []string{"b"},
		},
		{
			name:  "append unique value to non-empty slice",
			input: []string{"x", "y"},
			value: "z",
			want:  []string{"x", "y", "z"},
		},
		{
			name:  "do not append duplicate value (value already present)",
			input: []string{"alpha", "beta", "gamma"},
			value: "beta",
			want:  []string{"alpha", "beta", "gamma"},
		},
		{
			name:  "multiple duplicates — value appears multiple times, returns original slice",
			input: []string{"a", "b", "a", "c", "a"},
			value: "a",
			want:  []string{"a", "b", "a", "c", "a"},
		},
		{
			name:  "case-sensitive comparison — different case is unique",
			input: []string{"Hello", "World"},
			value: "hello",
			want:  []string{"Hello", "World", "hello"},
		},
		{
			name:  "duplicate in first position",
			input: []string{"first", "second", "third"},
			value: "first",
			want:  []string{"first", "second", "third"},
		},
		{
			name:  "duplicate in last position",
			input: []string{"one", "two", "three"},
			value: "three",
			want:  []string{"one", "two", "three"},
		},
		{
			name:  "empty value as element — append empty string when not present",
			input: []string{"x", "y"},
			value: "",
			want:  []string{"x", "y", ""},
		},
		{
			name:  "empty value already present — do not duplicate empty string",
			input: []string{"", "x", "y"},
			value: "",
			want:  []string{"", "x", "y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.input, tt.value)
			if len(got) != len(tt.want) {
				t.Fatalf("appendUnique(%v, %q) length = %d, want %d (got %v, want %v)", tt.input, tt.value, len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("appendUnique(%v, %q)[%d] = %q, want %q (got %v, want %v)", tt.input, tt.value, i, got[i], tt.want[i], got, tt.want)
				}
			}
		})
	}
}

// newTestCipher creates a secrets.Cipher with a random 32-byte key for testing.
func newTestCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}
	return cipher
}

// newTestSnapshot creates a managementSnapshot with all 4 secret fields set to known plaintext values.
func newTestSnapshot() managementSnapshot {
	return managementSnapshot{
		Settings: Settings{
			NaivePassword:     "naive-password-plain",
			Hysteria2Password: "hysteria2-password-plain",
		},
		Warp: WarpConfig{
			LicenseKey: "warp-license-plain",
			PrivateKey: "warp-private-plain",
		},
	}
}

func TestEncryptSnapshotEncryptsAllPlaintextFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	s.encryptSnapshot(&snapshot)

	// All 4 fields should be encrypted (start with "ve1:")
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("NaivePassword was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.LicenseKey) {
		t.Fatal("Warp.LicenseKey was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey was not encrypted")
	}

	// Verify each field decrypts back to original plaintext
	if dec, err := cipher.Decrypt(snapshot.Settings.NaivePassword); err != nil {
		t.Fatalf("failed to decrypt NaivePassword: %v", err)
	} else if dec != "naive-password-plain" {
		t.Fatalf("decrypted NaivePassword = %q, want %q", dec, "naive-password-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Settings.Hysteria2Password); err != nil {
		t.Fatalf("failed to decrypt Hysteria2Password: %v", err)
	} else if dec != "hysteria2-password-plain" {
		t.Fatalf("decrypted Hysteria2Password = %q, want %q", dec, "hysteria2-password-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Warp.LicenseKey); err != nil {
		t.Fatalf("failed to decrypt Warp.LicenseKey: %v", err)
	} else if dec != "warp-license-plain" {
		t.Fatalf("decrypted Warp.LicenseKey = %q, want %q", dec, "warp-license-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Warp.PrivateKey); err != nil {
		t.Fatalf("failed to decrypt Warp.PrivateKey: %v", err)
	} else if dec != "warp-private-plain" {
		t.Fatalf("decrypted Warp.PrivateKey = %q, want %q", dec, "warp-private-plain")
	}
}

func TestEncryptSnapshotSkipsAlreadyEncryptedFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	alreadyEncrypted := "ve1:some-already-encrypted-value"
	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     alreadyEncrypted,
			Hysteria2Password: "hysteria2-password-plain",
		},
		Warp: WarpConfig{
			LicenseKey: alreadyEncrypted,
			PrivateKey: "warp-private-plain",
		},
	}

	s.encryptSnapshot(&snapshot)

	// Already-encrypted fields must remain unchanged
	if snapshot.Settings.NaivePassword != alreadyEncrypted {
		t.Fatalf("already-encrypted NaivePassword was modified: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Warp.LicenseKey != alreadyEncrypted {
		t.Fatalf("already-encrypted Warp.LicenseKey was modified: %q", snapshot.Warp.LicenseKey)
	}

	// Plaintext fields should be encrypted
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password should have been encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey should have been encrypted")
	}
}

func TestEncryptSnapshotSkipsEmptyFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     "",
			Hysteria2Password: "",
		},
		Warp: WarpConfig{
			LicenseKey: "",
			PrivateKey: "",
		},
	}

	s.encryptSnapshot(&snapshot)

	// Empty fields must remain empty
	if snapshot.Settings.NaivePassword != "" {
		t.Fatalf("empty NaivePassword was modified: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "" {
		t.Fatalf("empty Hysteria2Password was modified: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "" {
		t.Fatalf("empty Warp.LicenseKey was modified: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "" {
		t.Fatalf("empty Warp.PrivateKey was modified: %q", snapshot.Warp.PrivateKey)
	}
}

func TestEncryptSnapshotNoopWhenCipherIsNil(t *testing.T) {
	s := &managementState{cipher: nil} // cipher is nil
	snapshot := newTestSnapshot()

	s.encryptSnapshot(&snapshot)

	// All fields should remain as plaintext (no-op)
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword was modified with nil cipher: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password was modified with nil cipher: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey was modified with nil cipher: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey was modified with nil cipher: %q", snapshot.Warp.PrivateKey)
	}
}

func TestDecryptSnapshotRestoresPlaintext(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	// First encrypt
	s.encryptSnapshot(&snapshot)

	// Verify they're encrypted
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("expected NaivePassword to be encrypted before decrypt")
	}

	// Then decrypt
	s.decryptSnapshot(&snapshot)

	// All fields should be back to plaintext
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("decrypted NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "naive-password-plain")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("decrypted Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-password-plain")
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("decrypted Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-plain")
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("decrypted Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-private-plain")
	}
}

func TestDecryptSnapshotNoopWhenCipherIsNil(t *testing.T) {
	s := &managementState{cipher: nil}
	snapshot := newTestSnapshot()

	s.decryptSnapshot(&snapshot)

	// All fields should remain unchanged
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword was modified with nil cipher: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password was modified with nil cipher: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey was modified with nil cipher: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey was modified with nil cipher: %q", snapshot.Warp.PrivateKey)
	}
}

func TestDecryptSnapshotPassesThroughPlaintext(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	// decryptSnapshot without encrypting first — plaintext should pass through unchanged
	s.decryptSnapshot(&snapshot)

	// All plaintext fields should remain unchanged (pass through)
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "naive-password-plain")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-password-plain")
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-plain")
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-private-plain")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	// Use a richer snapshot with all 4 fields populated
	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     "super-secret-naive-pass-123",
			Hysteria2Password: "hysteria2-!@#$%^&*()-pass",
		},
		Warp: WarpConfig{
			LicenseKey: "warp-license-abcdefghijklmnop",
			PrivateKey: "warp-priv-key-0123456789",
		},
	}

	// Encrypt
	s.encryptSnapshot(&snapshot)

	// Verify all encrypted
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("NaivePassword not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Warp.LicenseKey) {
		t.Fatal("Warp.LicenseKey not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey not encrypted after encrypt")
	}

	// Decrypt
	s.decryptSnapshot(&snapshot)

	// Verify full roundtrip restores original values
	if snapshot.Settings.NaivePassword != "super-secret-naive-pass-123" {
		t.Fatalf("roundtrip NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "super-secret-naive-pass-123")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-!@#$%^&*()-pass" {
		t.Fatalf("roundtrip Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-!@#$%^&*()-pass")
	}
	if snapshot.Warp.LicenseKey != "warp-license-abcdefghijklmnop" {
		t.Fatalf("roundtrip Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-abcdefghijklmnop")
	}
	if snapshot.Warp.PrivateKey != "warp-priv-key-0123456789" {
		t.Fatalf("roundtrip Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-priv-key-0123456789")
	}
}

func TestHandleSettingsRejectsFallbackRootPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a key file so newManagementState can load it
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	statePath := filepath.Join(tmpDir, "state.json")
	state := newManagementState(ServerInfo{
		StatePath: statePath,
		KeyPath:   keyPath,
		Mode:      "dev",
	})

	mux := http.NewServeMux()
	state.register(mux)

	validBody := func(fallbackRoot string) []byte {
		return []byte(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"dev","fallbackRoot":"` + fallbackRoot + `"}`)
	}

	tests := []struct {
		name         string
		fallbackRoot string
		wantStatus   int
		checkRoot    bool // whether to expect fallbackRoot in response
	}{
		{"PUT /var/lib/veil/www → 200", "/var/lib/veil/www", http.StatusOK, true},
		{"PUT /var/lib/veil/custom/path → 200", "/var/lib/veil/custom/path", http.StatusOK, true},
		{"PUT /etc/passwd → 400", "/etc/passwd", http.StatusBadRequest, false},
		{"PUT /var/lib/veil/../../../etc → 400", "/var/lib/veil/../../../etc", http.StatusBadRequest, false},
		{"PUT empty → 200", "", http.StatusOK, false},
		{"PUT relative/path → 200", "relative/path", http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(validBody(tt.fallbackRoot)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.checkRoot {
				if !strings.Contains(rec.Body.String(), `"fallbackRoot"`) {
					t.Fatalf("response should contain fallbackRoot, got: %s", rec.Body.String())
				}
			}
		})
	}
}
