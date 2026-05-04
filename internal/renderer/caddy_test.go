package renderer

import (
	"strings"
	"testing"
)

func TestRenderNaiveCaddyfile(t *testing.T) {
	cfg, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:       "example.com",
		Email:        "admin@example.com",
		ListenPort:   443,
		Username:     "alice",
		Password:     "secret",
		FallbackRoot: "/var/lib/veil/www",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"order forward_proxy before file_server",
		"servers {",
		"protocols h1 h2",
		":443, example.com",
		"tls admin@example.com",
		"basic_auth alice secret",
		"hide_ip",
		"hide_via",
		"probe_resistance",
		"root * /var/lib/veil/www",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("rendered Caddyfile missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderNaiveCaddyfileRequiresDomain(t *testing.T) {
	_, err := RenderNaiveCaddyfile(NaiveConfig{
		ListenPort: 443,
		Username:   "alice",
		Password:   "secret",
	})
	if err == nil {
		t.Fatal("expected error for missing domain, got nil")
	}
	if !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("expected 'domain is required', got: %v", err)
	}
}

func TestRenderNaiveCaddyfileRequiresListenPort(t *testing.T) {
	_, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:   "example.com",
		Username: "alice",
		Password: "secret",
	})
	if err == nil {
		t.Fatal("expected error for missing listen port, got nil")
	}
	if !strings.Contains(err.Error(), "listen port is required") {
		t.Fatalf("expected 'listen port is required', got: %v", err)
	}
}

func TestRenderNaiveCaddyfileRequiresCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"both empty", "", ""},
		{"username empty", "", "secret"},
		{"password empty", "alice", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderNaiveCaddyfile(NaiveConfig{
				Domain:     "example.com",
				ListenPort: 443,
				Username:   tt.username,
				Password:   tt.password,
			})
			if err == nil {
				t.Fatal("expected error for missing credentials, got nil")
			}
			if !strings.Contains(err.Error(), "username and password are required") {
				t.Fatalf("expected 'username and password are required', got: %v", err)
			}
		})
	}
}

func TestRenderNaiveCaddyfileDefaultsFallbackRoot(t *testing.T) {
	cfg, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:     "example.com",
		Email:      "admin@example.com",
		ListenPort: 443,
		Username:   "alice",
		Password:   "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cfg, "root * /var/lib/veil/www") {
		t.Fatalf("expected default fallback root, got:\n%s", cfg)
	}
}

func TestRenderNaiveCaddyfileRejectsZeroListenPort(t *testing.T) {
	_, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:     "example.com",
		ListenPort: 0,
		Username:   "alice",
		Password:   "secret",
	})
	if err == nil {
		t.Fatal("expected error for zero listen port, got nil")
	}
	if !strings.Contains(err.Error(), "listen port is required") {
		t.Fatalf("expected 'listen port is required', got: %v", err)
	}
}

func TestRenderNaiveCaddyfileNormalizesPaths(t *testing.T) {
	tests := []struct {
		name         string
		fallbackRoot string
		wantOk       bool
	}{
		{"FallbackRoot=/etc/passwd normalizes into /var/lib/veil", "/etc/passwd", true},
		{"FallbackRoot=/var/lib/veil/../../../etc/passwd normalizes into /var/lib/veil", "/var/lib/veil/../../../etc/passwd", true},
		{"FallbackRoot=/var/lib/veil/www unchanged", "/var/lib/veil/www", true},
		{"FallbackRoot empty uses default", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderNaiveCaddyfile(NaiveConfig{
				Domain:       "example.com",
				Email:        "admin@example.com",
				ListenPort:   443,
				Username:     "alice",
				Password:     "secret",
				FallbackRoot: tt.fallbackRoot,
			})
			if tt.wantOk && err != nil {
				t.Fatalf("unexpected error for FallbackRoot=%q: %v", tt.fallbackRoot, err)
			}
			if !tt.wantOk && err == nil {
				t.Fatalf("expected error for FallbackRoot=%q, got nil", tt.fallbackRoot)
			}
		})
	}
}
