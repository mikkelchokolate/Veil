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
		Settings:  model.Settings{Domain: "example.com"},
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

	// Verify naiveproxy configs are rendered as separate files
	naiveAPath := filepath.Join(root, "generated", "caddy", "naive-a.Caddyfile")
	naiveBPath := filepath.Join(root, "generated", "caddy", "naive-b.Caddyfile")
	caddyContentA, ok := configs[naiveAPath]
	if !ok {
		t.Fatalf("missing caddy config at %s", naiveAPath)
	}
	caddyContentB, ok := configs[naiveBPath]
	if !ok {
		t.Fatalf("missing caddy config at %s", naiveBPath)
	}

	if !strings.Contains(caddyContentA, "usera") {
		t.Errorf("Caddyfile does not contain naive proxy definition for usera: %s", caddyContentA)
	}
	if !strings.Contains(caddyContentB, "userb") {
		t.Errorf("Caddyfile does not contain naive proxy definition for userb: %s", caddyContentB)
	}
}
