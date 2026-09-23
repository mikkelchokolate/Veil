package protocols

import (
	"encoding/base64"
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

	// Verify hysteria2 configs are rendered as separate files — and that each
	// body actually carries its own listen port and credential, not just that
	// the map keys exist (#894).
	hy2APath := filepath.Join(root, "generated", "hysteria2", "hy2-a.yaml")
	hy2BPath := filepath.Join(root, "generated", "hysteria2", "hy2-b.yaml")
	hy2A, ok := configs[hy2APath]
	if !ok {
		t.Fatalf("missing config for hy2-a at %s", hy2APath)
	}
	hy2B, ok := configs[hy2BPath]
	if !ok {
		t.Fatalf("missing config for hy2-b at %s", hy2BPath)
	}
	for _, want := range []string{"listen: :10001", "password: passa"} {
		if !strings.Contains(hy2A, want) {
			t.Errorf("hy2-a config missing %q:\n%s", want, hy2A)
		}
	}
	for _, want := range []string{"listen: :10002", "password: passb"} {
		if !strings.Contains(hy2B, want) {
			t.Errorf("hy2-b config missing %q:\n%s", want, hy2B)
		}
	}
	if strings.Contains(hy2A, "passb") || strings.Contains(hy2B, "passa") {
		t.Fatalf("hysteria2 per-inbound credentials crossed: hy2-a=%q hy2-b=%q", hy2A, hy2B)
	}

	// Verify naiveproxy config is rendered as a single consolidated JSON file
	naivePath := filepath.Join(root, "generated", "caddy", "config.json")
	caddyContent, ok := configs[naivePath]
	if !ok {
		t.Fatalf("missing caddy config at %s", naivePath)
	}

	if !strings.Contains(caddyContent, forwardProxyJSONCredential("usera", "passa")) {
		t.Errorf("Caddy JSON does not contain naive proxy credential for usera: %s", caddyContent)
	}
	if !strings.Contains(caddyContent, forwardProxyJSONCredential("userb", "passb")) {
		t.Errorf("Caddy JSON does not contain naive proxy credential for userb: %s", caddyContent)
	}
}

func forwardProxyJSONCredential(username, password string) string {
	basicValue := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return base64.StdEncoding.EncodeToString([]byte(basicValue))
}
