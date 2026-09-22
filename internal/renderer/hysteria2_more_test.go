package renderer

import (
	"os"
	"path/filepath"
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
		"- name: warp",
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
			Match:    `geosite:category-gov-ru,suffix:ru,regexp:.*\.su$,full:api.example.com`,
			Outbound: "direct",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: direct",
		"type: direct",
		"name: warp",
		"acl:",
		"direct(suffix:ru)",
		"direct(api.example.com)",
		"warp(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	// geosite.dat is absent in this test, so geosite() must not crash hysteria2.
	if strings.Contains(cfg, "geosite:category-gov-ru") {
		t.Fatalf("geosite ACL requires geosite.dat:\n%s", cfg)
	}
	// #679/#680: Hysteria has no keyword/regexp/domain/cidr matchers — the
	// management dialect prefixes must never reach acl.inline.
	for _, bad := range []string{"regex:", "regexp:", "keyword:", "domain:", "cidr:"} {
		if strings.Contains(cfg, bad) {
			t.Fatalf("non-Hysteria ACL dialect %q emitted:\n%s", bad, cfg)
		}
	}
}

func TestRenderHysteria2ACLIncludesGeositeWhenDatExists(t *testing.T) {
	dir := t.TempDir()
	geoip := filepath.Join(dir, "geoip.dat")
	geosite := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(geoip, []byte("geoip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geosite, []byte("geosite"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		GeoIPPath:     geoip,
		GeoSitePath:   geosite,
		RoutingRules: []Hysteria2RoutingRule{{
			Match:    `geosite:category-gov-ru,full:api.example.com,10.9.0.0/16`,
			Outbound: "direct",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"direct(geosite:category-gov-ru)",
		"direct(api.example.com)",
		"direct(10.9.0.0/16)",
		"warp(all)",
		"geosite: " + geosite,
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
}

func TestRenderHysteria2ACLAllOnlyProxyKeepsNamedOutbound(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		RoutingRules:  []Hysteria2RoutingRule{{Match: "all", Outbound: "proxy"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: proxy",
		"name: warp",
		"proxy(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "warp(all)") {
		t.Fatalf("explicit all→proxy must not collapse to warp-only:\n%s", cfg)
	}
}

func TestRenderHysteria2ACLAllOnlyDirectKeepsNamedOutbound(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		RoutingRules:  []Hysteria2RoutingRule{{Match: "all", Outbound: "direct"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: direct",
		"name: warp",
		"direct(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "warp(all)") {
		t.Fatalf("explicit all→direct must not collapse to warp-only:\n%s", cfg)
	}
}

func TestRenderHysteria2ACLKeepsProxyOffWarp(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
		Upstream:      "127.0.0.1:40000",
		RoutingRules: []Hysteria2RoutingRule{
			{Match: `regexp:.*\.ru$,suffix:ru`, Outbound: "direct"},
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
		"name: warp",
		"direct(suffix:ru)",
		"warp(suffix:openai.com)",
		"proxy(all)",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	// The regexp atom is not expressible in Hysteria ACL and must be dropped,
	// not emitted as a fake regex: prefix.
	if strings.Contains(cfg, "regex:") || strings.Contains(cfg, "regexp:") {
		t.Fatalf("regexp matcher must not reach acl.inline:\n%s", cfg)
	}
	if strings.Contains(cfg, "warp(all)") {
		t.Fatalf("proxy must not fall through to WARP:\n%s", cfg)
	}
}
