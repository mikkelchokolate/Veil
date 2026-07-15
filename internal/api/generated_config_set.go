package api

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/panelaccess"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type GeneratedConfigInput struct {
	ApplyRoot string
	LiveRoot  string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	return generatedconfig.NewSetBuilder(generatedconfig.SetInput{
		ApplyRoot:  input.ApplyRoot,
		LiveRoot:   input.LiveRoot,
		Settings:   input.Settings,
		Inbounds:   input.Inbounds,
		WarpConfig: input.Warp,
		Registry:   protocols.NewGeneratedConfigRegistry(),
		PanelAccess: func(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return panelaccess.New(input.Settings, protocols.NewCatalog().RequiresCaddy).GeneratedConfig(paths)
		},
		Warp: func(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return generatedconfig.NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
		},
	}).Build()
}
