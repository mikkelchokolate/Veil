package api

import "github.com/veil-panel/veil/internal/renderer"

type GeneratedConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	configs, err := NewGeneratedConfigProtocolRegistry().Render(input)
	if err != nil {
		return nil, err
	}
	paths := NewGeneratedConfigPaths(input.ApplyRoot)
	if input.Settings.PanelAccess == "caddy" {
		if _, exists := configs[paths.Caddyfile()]; !exists {
			route, _, err := NewPanelCaddyAccess().Route(input.Settings)
			if err != nil {
				return nil, err
			}
			body, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: input.Settings.Domain, Email: input.Settings.Email, PanelPort: route.Port, WebBasePath: route.WebBasePath})
			if err != nil {
				return nil, err
			}
			configs[paths.Caddyfile()] = body
		}
	}
	artifact, ok, err := NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
	if err != nil {
		return nil, err
	}
	if ok {
		configs[artifact.Path] = artifact.Body
	}
	return configs, nil
}
