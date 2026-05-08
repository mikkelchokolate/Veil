package api

import (
	"bytes"
	"crypto/rand"
	"fmt"
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

var _managementTestDeps_service_validation = []any{
	bytes.Buffer{}, rand.Reader, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, time.Second, secrets.IsEncrypted,
}

func TestStackAllowsProtocolDoesNotSelectPanelProtocols(t *testing.T) {
	tests := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"panel", "naiveproxy", true},
		{"panel", "hysteria2", true},
		{"panel", "mieru", true},
		{"naive", "hysteria2", true},
		{"both", "mieru", true},
		{"unknown", "naiveproxy", true},
		{"panel", "unknown", false},
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

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		value string
		want  []string
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
