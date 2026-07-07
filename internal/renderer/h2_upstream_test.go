package renderer

import (
	"strings"
	"testing"
)

func TestRenderH2UsesOutboundsListForUpstream(t *testing.T) {
	body, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    8443,
		Password:      "secret",
		MasqueradeURL: "https://example.com/",
		Upstream:      "127.0.0.1:40001",
	})
	if err != nil {
		t.Fatalf("RenderHysteria2 returned error: %v", err)
	}
	for _, want := range []string{"outbounds:", "- name: veil-upstream", "addr: 127.0.0.1:40001"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "\noutbound:\n") {
		t.Fatalf("rendered config contains singular outbound block:\n%s", body)
	}
}
