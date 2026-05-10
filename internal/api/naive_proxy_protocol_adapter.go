package api

import "github.com/veil-panel/veil/internal/renderer"

type NaiveProxyProtocolAdapter struct{}

func (NaiveProxyProtocolAdapter) Capability() ProtocolCapability {
	return ProtocolCapability{
		Protocol:               "naiveproxy",
		DisplayName:            "NaiveProxy",
		Transports:             []string{"tcp"},
		FirewallService:        "Veil NaiveProxy",
		RequiresCaddy:          true,
		GeneratedConfig:        GeneratedConfigArtifactSpec{Subpath: generatedCaddyfileSubpath, ValidationName: "caddy", ValidationCommand: func(path string) []string { return []string{"caddy", "validate", "--config", path} }},
		ApplyAction:            "reload " + renderer.UnitNaive,
		RuntimeName:            "naive",
		RuntimeActionName:      "caddy",
		RuntimeUnit:            renderer.UnitNaive,
		RuntimeTransport:       "tcp",
		RuntimeOrder:           10,
		PromotedVerb:           "reload",
		ValidateInboundRender:  true,
		RequiresRenderSettings: true,
		RequiresCaddySettings:  true,
		MaxEnabled:             1,
		RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return GeneratedConfigArtifact{}, false, nil
			}
			body, err := renderNaiveGeneratedConfig(input.Settings, input.Inbounds[0])
			return GeneratedConfigArtifact{Path: input.Paths.Generated(generatedCaddyfileSubpath), Body: body}, true, err
		},
	}
}
