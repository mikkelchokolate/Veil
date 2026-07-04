package renderer

import (
	"strings"
	"testing"
)

func TestRenderNaiveCaddyfileRejectsEmptyUserInUsers(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"empty username", "", "pass"},
		{"empty password", "user", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderNaiveCaddyfile(NaiveConfig{
				Domain:     "example.com",
				ListenPort: 443,
				Users: []NaiveUser{{
					Username: tt.username,
					Password: tt.password,
				}},
			})
			if err == nil {
				t.Fatal("expected error for empty user credential")
			}
			if !strings.Contains(err.Error(), "username and password are required") {
				t.Fatalf("expected credential error, got: %v", err)
			}
		})
	}
}

func TestRenderNaiveCaddyfileRejectsPathTraversal(t *testing.T) {
	_, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:       "example.com",
		ListenPort:   443,
		Username:     "alice",
		Password:     "secret",
		FallbackRoot: "../../../etc/passwd",
	})
	if err == nil {
		t.Fatal("expected error for path traversal fallback root")
	}
	if !strings.Contains(err.Error(), "fallback root must be within /var/lib/veil") {
		t.Fatalf("expected path traversal error, got: %v", err)
	}
}

func TestRenderNaiveCaddyfileUpstream(t *testing.T) {
	cfg, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:       "example.com",
		ListenPort:   443,
		Username:     "alice",
		Password:     "secret",
		FallbackRoot: "/var/lib/veil/www",
		Upstream:     "socks5://127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cfg, "upstream socks5://127.0.0.1:1080") {
		t.Fatalf("expected upstream directive:\n%s", cfg)
	}
}
