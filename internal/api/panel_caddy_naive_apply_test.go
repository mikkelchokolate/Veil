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
	caddyfile := configs[filepath.Join(root, "generated", "caddy", "panel.Caddyfile")]
	for _, want := range []string{"panel.example.com", "tls admin@example.com", "handle_path /panel-secret/*", "reverse_proxy 127.0.0.1:2096"} {
		if !strings.Contains(caddyfile, want) {
			t.Fatalf("Panel Caddyfile missing %q:\n%s", want, caddyfile)
		}
	}
	if strings.Contains(caddyfile, "forward_proxy") {
		t.Fatalf("Panel-only Caddyfile should not include NaiveProxy forward_proxy:\n%s", caddyfile)
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
	caddyfile := configs[filepath.Join(root, "generated", "caddy", "naive.Caddyfile")]
	for _, want := range []string{"forward_proxy", "basic_auth veil naive-secret", "handle_path /panel-secret/*", "reverse_proxy 127.0.0.1:2096"} {
		if !strings.Contains(caddyfile, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, caddyfile)
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
	caddyfile := configs[filepath.Join(root, "generated", "caddy", "naive.Caddyfile")]
	if strings.Contains(caddyfile, "reverse_proxy 127.0.0.1:2096") || strings.Contains(caddyfile, "handle_path /panel-secret/*") {
		t.Fatalf("Caddyfile should not include Panel Caddy access route:\n%s", caddyfile)
	}
}
