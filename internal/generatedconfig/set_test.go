package generatedconfig

import "testing"

func TestSetBuilderCombinesProtocolPanelFallbackAndWarpArtifacts(t *testing.T) {
	paths := NewPaths("/etc/veil")
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry: NewProtocolRegistry([]Protocol{{Protocol: "mieru", Render: func(input ProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{Path: input.Paths.Mieru(), Body: "mieru"}, true, nil
		}}}),
		PanelAccess: func(Paths) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{Path: paths.Caddyfile(), Body: "panel-caddy"}, true, nil
		},
		Warp: func(Paths) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{Path: paths.Warp(), Body: "warp"}, true, nil
		},
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Enabled: true}},
	})
	configs, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if configs[paths.Mieru()] != "mieru" || configs[paths.Caddyfile()] != "panel-caddy" || configs[paths.Warp()] != "warp" {
		t.Fatalf("configs = %+v", configs)
	}
}

func TestSetBuilderDoesNotOverwriteProtocolCaddyWithPanelFallback(t *testing.T) {
	paths := NewPaths("/etc/veil")
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry: NewProtocolRegistry([]Protocol{{Protocol: "naiveproxy", Render: func(input ProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{Path: input.Paths.Caddyfile(), Body: "naive-caddy"}, true, nil
		}}}),
		PanelAccess: func(Paths) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{Path: paths.Caddyfile(), Body: "panel-caddy"}, true, nil
		},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Enabled: true}},
	})
	configs, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if configs[paths.Caddyfile()] != "naive-caddy" {
		t.Fatalf("caddy config = %q", configs[paths.Caddyfile()])
	}
}
