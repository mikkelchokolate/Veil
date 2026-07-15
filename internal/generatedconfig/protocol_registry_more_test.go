package generatedconfig

import (
	"errors"
	"testing"
)

func TestProtocolRegistryRenderSkipsProtocolsWithNoEnabledInbounds(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			t.Fatal("mieru render should not be called without enabled inbounds")
			return nil, false, nil
		}},
	})
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{{Name: "hy2", Protocol: "hysteria2", Enabled: true}}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected no configs, got %+v", configs)
	}
}

func TestProtocolRegistryRenderSkipsWhenRenderSettingsMissing(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "naiveproxy", RequiresRenderSettings: true, Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			t.Fatal("render should not be called without render settings")
			return nil, false, nil
		}},
	})
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Settings: Settings{}, Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Enabled: true}}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected no configs without render settings, got %+v", configs)
	}
}

func TestProtocolRegistryRenderPropagatesRenderError(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return nil, false, errors.New("render failed")
		}},
	})
	_, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Enabled: true}}})
	if err == nil || err.Error() != "render failed" {
		t.Fatalf("expected render error, got %v", err)
	}
}

func TestProtocolRegistryRenderCollectsMultipleArtifacts(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return []GeneratedConfigArtifact{
				{Path: input.Paths.Mieru(), Body: "one"},
				{Path: input.Paths.CaddyJSON(), Body: "two"},
			}, true, nil
		}},
	})
	paths := NewPaths("/etc/veil")
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Enabled: true}}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if configs[paths.Mieru()] != "one" || configs[paths.CaddyJSON()] != "two" {
		t.Fatalf("configs = %+v", configs)
	}
}

func TestProtocolRegistryRenderInboundUnknownProtocol(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "mieru"}})
	_, ok, err := registry.RenderInbound(Settings{}, NewPaths("/etc/veil"), Inbound{Name: "x", Protocol: "unknown", Enabled: true}, WarpConfig{})
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestProtocolRegistryRenderInboundRenderError(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return nil, false, errors.New("render failed")
		}},
	})
	_, _, err := registry.RenderInbound(Settings{}, NewPaths("/etc/veil"), Inbound{Name: "mieru", Protocol: "mieru", Enabled: true}, WarpConfig{})
	if err == nil || err.Error() != "render failed" {
		t.Fatalf("expected render error, got %v", err)
	}
}

func TestProtocolRegistryRenderInboundHandlesEmptyArtifactList(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return []GeneratedConfigArtifact{}, true, nil
		}},
	})
	artifact, ok, err := registry.RenderInbound(Settings{}, NewPaths("/etc/veil"), Inbound{Name: "mieru", Protocol: "mieru", Enabled: true}, WarpConfig{})
	if err != nil || !ok || artifact.Path != "" || artifact.Body != "" {
		t.Fatalf("artifact=%+v ok=%v err=%v", artifact, ok, err)
	}
}

func TestProtocolRegistryRenderInboundReturnsArtifact(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return []GeneratedConfigArtifact{{Path: input.Paths.Mieru(), Body: "mieru"}}, true, nil
		}},
	})
	artifact, ok, err := registry.RenderInbound(Settings{}, NewPaths("/etc/veil"), Inbound{Name: "mieru", Protocol: "mieru", Enabled: true}, WarpConfig{})
	if err != nil || !ok || artifact.Body != "mieru" {
		t.Fatalf("artifact=%+v ok=%v err=%v", artifact, ok, err)
	}
}

func TestProtocolRegistryRenderUsesProtocolFieldsForRenderSettings(t *testing.T) {
	called := false
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "hysteria2", RequiresRenderSettings: true, Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			called = true
			return []GeneratedConfigArtifact{{Path: input.Paths.Generated("hysteria2/server.yaml"), Body: "hy2"}}, true, nil
		}},
	})
	settings := Settings{ProtocolFields: map[string]any{"hysteria2Password": "secret"}}
	inbounds := []Inbound{{Name: "hy2", Protocol: "hysteria2", Enabled: true}}
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Settings: settings, Inbounds: inbounds})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !called {
		t.Fatal("render should be called when protocolFields provide render settings")
	}
	if configs[NewPaths("/etc/veil").Generated("hysteria2/server.yaml")] != "hy2" {
		t.Fatalf("configs = %+v", configs)
	}
}
