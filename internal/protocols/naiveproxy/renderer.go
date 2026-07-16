package naiveproxy

import (
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates the consolidated Caddy JSON config for all naiveproxy
// inbounds (and panel access when panelAccess is caddy). The inbound/Caddy
// redesign moves from per-inbound Caddyfiles to a single JSON config used by
// veil-caddy.service.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}

	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(input.Settings, input.Inbounds)
	if err != nil {
		return nil, false, err
	}

	caps, err := caddycapabilities.Probe("")
	if err != nil {
		return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
	}
	data, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return nil, false, err
	}

	return []generatedconfig.GeneratedConfigArtifact{{
		Path: input.Paths.CaddyJSON(),
		Body: string(data),
	}}, true, nil
}

// ArtifactSpec returns the artifact metadata for the consolidated Caddy JSON
// config.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.CaddyJSONConfigSubpath,
		ValidationName: "caddy",
		ValidationCommand: func(path string) []string {
			return []string{"caddy", "validate", "--config", path}
		},
	}
}
