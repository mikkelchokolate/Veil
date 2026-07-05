package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedConfigSetRendersPanelCaddyAccessWithoutNaiveInbound(t *testing.T) {
	root := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: root,
		Settings: Settings{
			PanelListen: "127.0.0.1:2096",
			PanelAccess: "caddy",
			WebBasePath: "/panel-secret/",
			Mode:        "server",
			Domain:      "panel.example.com",
			Email:       "admin@example.com",
		},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddyJSON := configs[filepath.Join(root, "generated", "caddy", "config.json")]
	for _, want := range []string{"panel.example.com", `"handler": "reverse_proxy"`, `"dial": "127.0.0.1:2096"`} {
		if !strings.Contains(caddyJSON, want) {
			t.Fatalf("Panel Caddy JSON missing %q:\n%s", want, caddyJSON)
		}
	}
	if strings.Contains(caddyJSON, `"handler": "forward_proxy"`) {
		t.Fatalf("Panel-only Caddy JSON should not include NaiveProxy forward_proxy:\n%s", caddyJSON)
	}
}

func TestNaiveGeneratedConfigPreservesPanelCaddyAccessRoute(t *testing.T) {
	root := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: root,
		Settings: Settings{
			PanelListen:   "127.0.0.1:2096",
			PanelAccess:   "caddy",
			WebBasePath:   "/panel-secret/",
			Mode:          "server",
			Domain:        "vpn.example.com",
			Email:         "admin@example.com",
			NaiveUsername: "veil",
			NaivePassword: "naive-secret",
			FallbackRoot:  "/var/lib/veil/www",
		},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddyJSON := configs[filepath.Join(root, "generated", "caddy", "config.json")]
	for _, want := range []string{`"handler": "forward_proxy"`, `"handler": "reverse_proxy"`, `"dial": "127.0.0.1:2096"`} {
		if !strings.Contains(caddyJSON, want) {
			t.Fatalf("Caddy JSON missing %q:\n%s", want, caddyJSON)
		}
	}
}

func TestNaiveGeneratedConfigWithoutPanelCaddyDoesNotExposePanelRoute(t *testing.T) {
	root := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: root,
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Mode: "server", Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil", NaivePassword: "naive-secret", FallbackRoot: "/var/lib/veil/www"},
		Inbounds:  []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddyJSON := configs[filepath.Join(root, "generated", "caddy", "config.json")]
	if strings.Contains(caddyJSON, `"dial": "127.0.0.1:2096"`) || strings.Contains(caddyJSON, `"path": ["/panel-secret/`) {
		t.Fatalf("Caddy JSON should not include Panel Caddy access route:\n%s", caddyJSON)
	}
}
