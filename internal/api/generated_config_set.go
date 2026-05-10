package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/panelaccess"
	"github.com/veil-panel/veil/internal/protocols"
)

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
			return panelaccess.New(input.Settings, protocols.NewCatalog().RequiresCaddy).GeneratedConfig(paths)
		},
		Warp: func(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return generatedconfig.NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
		},
	}).Build()
}
