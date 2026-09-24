package renderer

import (
	"strings"
	"testing"
)

func TestRenderPanelCaddyfileProxiesOnlyPanelBasePath(t *testing.T) {
	body, err := RenderPanelCaddyfile(PanelCaddyConfig{Domain: "example.com", Email: "admin@example.com", PanelPort: 2096, WebBasePath: "/panel-secret/"})
	if err != nil {
		t.Fatalf("RenderPanelCaddyfile: %v", err)
	}
	for _, want := range []string{
		"example.com",
		"issuer acme",
		"email admin@example.com",
		"issuer internal",
		"handle /panel-secret/*",
		"reverse_proxy 127.0.0.1:2096",
		// HSTS must be emitted at the public TLS edge: the Go handler behind
		// reverse_proxy only sees loopback HTTP and cannot send it (#902).
		`header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel Caddyfile missing %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"forward_proxy", "basic_auth", "probe_resistance"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("panel Caddyfile must not include NaiveProxy directive %q:\n%s", unwanted, body)
		}
	}
}
