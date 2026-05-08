package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNaiveGeneratedConfigPreservesPanelCaddyAccessRoute(t *testing.T) {
	root := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: root,
		Settings: Settings{
			PanelListen:   "127.0.0.1:2096",
			PanelAccess:   "caddy",
			WebBasePath:   "/panel-secret/",
			Stack:         "panel",
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
	caddyfile := configs[filepath.Join(root, "generated", "caddy", "Caddyfile")]
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
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Stack: "panel", Mode: "server", Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil", NaivePassword: "naive-secret", FallbackRoot: "/var/lib/veil/www"},
		Inbounds:  []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddyfile := configs[filepath.Join(root, "generated", "caddy", "Caddyfile")]
	if strings.Contains(caddyfile, "reverse_proxy 127.0.0.1:2096") || strings.Contains(caddyfile, "handle_path /panel-secret/*") {
		t.Fatalf("Caddyfile should not include Panel Caddy access route:\n%s", caddyfile)
	}
}
