package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	registry := NewGeneratedConfigProtocolRegistry()
	return generatedconfig.NewSetBuilder(generatedconfig.SetInput{
		ApplyRoot: input.ApplyRoot,
		Settings:  input.Settings,
		Inbounds:  input.Inbounds,
		Registry:  registry.inner,
		PanelAccess: func(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return NewPanelAccess(input.Settings).GeneratedConfig(paths)
		},
		Warp: func(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
		},
	}).Build()
}
