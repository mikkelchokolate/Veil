package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
