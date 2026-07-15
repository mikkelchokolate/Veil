package protocols

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestGeneratedConfigRegistryOwnsCardinalityAndMieruRendering(t *testing.T) {
	registry := NewGeneratedConfigRegistry()
	artifact, ok, err := registry.RenderInbound(model.Settings{}, generatedconfig.NewPaths(t.TempDir()), model.Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 9443, Enabled: true, Password: "secret"}, generatedconfig.WarpConfig{})
	if err != nil {
		t.Fatalf("RenderInbound: %v", err)
	}
	if !ok || !strings.Contains(artifact.Body, `"protocol": "TCP"`) || !strings.Contains(artifact.Body, `"password": "secret"`) {
		t.Fatalf("single inbound artifact = %+v ok=%v", artifact, ok)
	}
	root := t.TempDir()
	configs, err := registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{},
		Inbounds:  []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 9443, Enabled: true, Password: "secret"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := configs[filepath.Join(root, "generated", "mieru", "server_config.json")]
	if !strings.Contains(body, `"port": 9443`) || !strings.Contains(body, `"protocol": "UDP"`) {
		t.Fatalf("bad Mieru generated config:\n%s", body)
	}
}

func TestGeneratedConfigRegistryMultipleHysteriaAndNaive(t *testing.T) {
	registry := NewGeneratedConfigRegistry()
	root := t.TempDir()

	inbounds := []model.Inbound{
		{Name: "hy2-a", Protocol: "hysteria2", Transport: "udp", Port: 10001, Enabled: true, Hysteria2Password: "passa"},
		{Name: "hy2-b", Protocol: "hysteria2", Transport: "udp", Port: 10002, Enabled: true, Hysteria2Password: "passb"},
		{Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 20001, Enabled: true, NaiveUsername: "usera", NaivePassword: "passa"},
		{Name: "naive-b", Protocol: "naiveproxy", Transport: "tcp", Port: 20002, Enabled: true, NaiveUsername: "userb", NaivePassword: "passb"},
	}

	configs, err := registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{Domain: "example.com", DefaultAcmeEmail: "admin@example.com"},
		Inbounds:  inbounds,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Verify hysteria2 configs are rendered as separate files
	hy2APath := filepath.Join(root, "generated", "hysteria2", "hy2-a.yaml")
	hy2BPath := filepath.Join(root, "generated", "hysteria2", "hy2-b.yaml")
	if _, ok := configs[hy2APath]; !ok {
		t.Errorf("missing config for hy2-a at %s", hy2APath)
	}
	if _, ok := configs[hy2BPath]; !ok {
		t.Errorf("missing config for hy2-b at %s", hy2BPath)
	}

	// Verify naiveproxy inbounds are rendered into the consolidated Caddy JSON config.
	naiveJSONPath := filepath.Join(root, "generated", "caddy", "config.json")
	caddyContent, ok := configs[naiveJSONPath]
	if !ok {
		t.Fatalf("missing consolidated caddy config at %s", naiveJSONPath)
	}

	if !strings.Contains(caddyContent, `"handler": "forward_proxy"`) {
		t.Errorf("Caddy JSON does not contain forward_proxy handler: %s", caddyContent)
	}
	if !strings.Contains(caddyContent, `"example.com"`) {
		t.Errorf("Caddy JSON does not contain expected domain: %s", caddyContent)
	}
}
