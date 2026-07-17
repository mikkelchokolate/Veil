package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementConfigRendererBuildsGeneratedConfigSet(t *testing.T) {
	root := t.TempDir()
	renderer := NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: root,
		Settings:  Settings{Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com", NaiveUsername: "veil", NaivePassword: "naive-pass", Hysteria2Password: "hy2-pass"},
		Inbounds:  []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})

	configs, err := renderer.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := configs[filepath.Join(root, "generated", "caddy", "config.json")]
	if !strings.Contains(body, "vpn.example.com") || !strings.Contains(body, "forward_proxy") {
		t.Fatalf("Caddy JSON missing domain or forward_proxy: %s", body)
	}
}

func TestManagementConfigRendererValidatesMieruInboundRender(t *testing.T) {
	renderer := NewManagementConfigRenderer(ManagementConfigInput{Settings: Settings{}})

	_, err := renderer.RenderInbound(Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true})

	if err == nil || err.Error() != "mieru user name and password are required" {
		t.Fatalf("expected Mieru render validation error, got %v", err)
	}
}
