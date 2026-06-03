package generatedconfig

import "testing"

func TestProtocolRegistryValidatesAndRendersEnabledInbounds(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "mieru", MaxEnabled: 1, Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return []GeneratedConfigArtifact{{Path: input.Paths.Mieru(), Body: input.Inbounds[0].Name}}, true, nil
		}},
	})
	_, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{
		{Name: "one", Protocol: "mieru", Enabled: true},
		{Name: "two", Protocol: "mieru", Enabled: true},
	}})
	if err == nil {
		t.Fatalf("expected max enabled validation error")
	}
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{{Name: "one", Protocol: "mieru", Enabled: true}}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if configs[NewPaths("/etc/veil").Mieru()] != "one" {
		t.Fatalf("configs = %+v", configs)
	}
}
