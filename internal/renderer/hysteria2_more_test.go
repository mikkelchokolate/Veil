package renderer

import (
	"strings"
	"testing"
)

func TestRenderHysteria2RejectsEmptyUserInUsers(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"empty username", "", "pass"},
		{"empty password", "alice", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderHysteria2(Hysteria2Config{
				ListenPort: 443,
				Users: []Hysteria2User{{
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

func TestRenderHysteria2DefaultsCertPaths(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Password:   "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"cert: /etc/veil/panel/tls.crt",
		"key: /etc/veil/panel/tls.key",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("expected %q in config:\n%s", want, cfg)
		}
	}
}

func TestRenderHysteria2Upstream(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"outbounds:",
		"- name: veil-upstream",
		"type: socks5",
		"addr: 127.0.0.1:1080",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("expected %q in config:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "\noutbound:\n") {
		t.Fatalf("expected no singular outbound block in config:\n%s", cfg)
	}
}

func TestRenderHysteria2ACLSplitsCommaSeparatedDirectRules(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		RoutingRules: []Hysteria2RoutingRule{{
			Match:    `geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`,
			Outbound: "direct",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: direct",
		"type: direct",
		"name: veil-upstream",
		"acl:",
		"direct(regex:.*\\.ru$)",
		"direct(regex:.*\\.su$)",
		"veil-upstream(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	// geosite.dat is absent in this test, so geosite() must not crash hysteria2.
	if strings.Contains(cfg, "geosite:category-gov-ru") {
		t.Fatalf("geosite ACL requires geosite.dat:\n%s", cfg)
	}
}

func TestRenderHysteria2ACLKeepsProxyOffWarp(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		RoutingRules: []Hysteria2RoutingRule{
			{Match: `regexp:.*\.ru$`, Outbound: "direct"},
			{Match: "domain:openai.com", Outbound: "warp"},
			{Match: "all", Outbound: "proxy"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: direct",
		"name: proxy",
		"name: veil-upstream",
		"direct(regex:.*\\.ru$)",
		"veil-upstream(suffix:openai.com)",
		"proxy(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "veil-upstream(all)") {
		t.Fatalf("proxy must not fall through to WARP:\n%s", cfg)
	}
}
