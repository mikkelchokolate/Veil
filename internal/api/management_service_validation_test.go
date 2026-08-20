package api

import (
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

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
			name:       "caddy config json",
			stagedPath: "/tmp/veil-test/generated/caddy/config.json",
			wantPath:   "/tmp/veil-test/live/caddy/config.json",
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
		{
			name:       "routing geosite.dat",
			stagedPath: "/tmp/veil-test/generated/rules/geosite.dat",
			wantPath:   "/tmp/veil-test/live/rules/geosite.dat",
			wantOK:     true,
		},
		// Unknown generated paths (valid prefix but not a known config)
		{
			name:       "legacy caddy Caddyfile",
			stagedPath: "/tmp/veil-test/generated/caddy/panel.Caddyfile",
			wantPath:   "",
			wantOK:     false,
		},
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
			stagedPath: "/tmp/veil-test/staged/caddy/config.json",
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "live directory instead of generated",
			stagedPath: "/tmp/veil-test/live/caddy/config.json",
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
			stagedPath: "generated/caddy/config.json",
			wantPath:   "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOK := state.livePathForStagedConfig(tt.stagedPath)
			gotPath = filepath.ToSlash(gotPath)
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

	gotPath, gotOK := state.livePathForStagedConfig("/tmp/veil-test/generated/caddy/config.json")
	gotPath = filepath.ToSlash(gotPath)
	wantPath := "/tmp/veil-test/live/caddy/config.json"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if !gotOK {
		t.Fatal("expected ok=true")
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
