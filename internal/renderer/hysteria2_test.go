package renderer

import (
	"strings"
	"testing"
)

func TestRenderHysteria2RecommendedConfig(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Domain: "example.com",
		Password: "secret-pass",
		MasqueradeURL: "https://www.bing.com/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"listen: :443",
		"type: password",
		"password: secret-pass",
		"type: proxy",
		"url: https://www.bing.com/",
		"rewriteHost: true",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, cfg)
		}
	}
}
