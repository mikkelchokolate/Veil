package renderer

import (
	"strings"
	"testing"
)

func TestRenderNaiveCaddyfile(t *testing.T) {
	cfg, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain: "example.com",
		Email: "admin@example.com",
		ListenPort: 443,
		Username: "alice",
		Password: "secret",
		FallbackRoot: "/var/lib/veil/www",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"order forward_proxy before file_server",
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
