package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedConfigProtocolRegistryOwnsSingleVsAggregateCardinality(t *testing.T) {
	registry := NewGeneratedConfigProtocolRegistry()
	settings := Settings{Stack: "both"}

	err := registry.Validate(settings, []Inbound{
		{Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "naive-b", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "multiple enabled naiveproxy inbounds") {
		t.Fatalf("expected single-config protocol cardinality error, got %v", err)
	}

	err = registry.Validate(settings, []Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "udp-pass"},
	})
	if err != nil {
		t.Fatalf("Mieru aggregate protocol should allow multiple Inbounds: %v", err)
	}
}

func TestGeneratedConfigProtocolRegistryRendersProtocolArtifacts(t *testing.T) {
	root := t.TempDir()
	configs, err := NewGeneratedConfigProtocolRegistry().Render(GeneratedConfigInput{
		ApplyRoot: root,
		Settings: Settings{
			Domain:            "vpn.example.com",
			Email:             "admin@example.com",
			Stack:             "both",
			NaiveUsername:     "veil",
			NaivePassword:     "naive-secret",
			Hysteria2Password: "hy2-secret",
			MasqueradeURL:     "https://www.bing.com/",
			FallbackRoot:      "/var/lib/veil/www",
		},
		Inbounds: []Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 8443, Enabled: true, Password: "mieru-secret"},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "generated", "caddy", "Caddyfile"),
		filepath.Join(root, "generated", "hysteria2", "server.yaml"),
		filepath.Join(root, "generated", "mieru", "server_config.json"),
	} {
		if configs[path] == "" {
			t.Fatalf("missing generated artifact %s in %+v", path, configs)
		}
	}
}

func TestGeneratedConfigProtocolRegistryRendersSingleInboundForPlanValidation(t *testing.T) {
	artifact, ok, err := NewGeneratedConfigProtocolRegistry().RenderInbound(
		Settings{Stack: "mieru"},
		NewGeneratedConfigPaths(t.TempDir()),
		Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 9443, Enabled: true, Password: "secret"},
	)
	if err != nil {
		t.Fatalf("RenderInbound: %v", err)
	}
	if !ok || !strings.Contains(artifact.Body, `"protocol": "TCP"`) || !strings.Contains(artifact.Body, `"password": "secret"`) {
		t.Fatalf("single inbound artifact = %+v ok=%v", artifact, ok)
	}
}

func TestGeneratedConfigProtocolRegistryRendersMieruWithoutSharedRenderSettings(t *testing.T) {
	root := t.TempDir()
	configs, err := NewGeneratedConfigProtocolRegistry().Render(GeneratedConfigInput{
		ApplyRoot: root,
		Settings:  Settings{Stack: "mieru"},
		Inbounds:  []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 9443, Enabled: true, Password: "secret"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := configs[filepath.Join(root, "generated", "mieru", "server_config.json")]
	if !strings.Contains(body, `"port": 9443`) || !strings.Contains(body, `"protocol": "UDP"`) {
		t.Fatalf("bad Mieru generated config:\n%s", body)
	}
}
