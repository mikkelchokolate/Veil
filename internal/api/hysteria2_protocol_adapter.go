package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/renderer"
)

type Hysteria2ProtocolAdapter struct{}

func (Hysteria2ProtocolAdapter) Capability() ProtocolCapability {
	return ProtocolCapability{
		Protocol:               "hysteria2",
		DisplayName:            "Hysteria2",
		Transports:             []string{"udp"},
		FirewallService:        "Veil Hysteria2",
		GeneratedConfig:        generatedconfig.ArtifactSpec{Subpath: generatedconfig.Hysteria2ConfigSubpath, ValidationName: "hysteria2", ValidationCommand: func(path string) []string { return []string{"hysteria", "server", "--config", path, "--check"} }},
		ApplyAction:            "reload " + renderer.UnitHysteria2,
		RuntimeName:            "hysteria2",
		RuntimeActionName:      "hysteria2",
		RuntimeUnit:            renderer.UnitHysteria2,
		RuntimeTransport:       "udp",
		RuntimeOrder:           20,
		PromotedVerb:           "reload",
		ValidateInboundRender:  true,
		RequiresRenderSettings: true,
		MaxEnabled:             1,
		RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return generatedconfig.GeneratedConfigArtifact{}, false, nil
			}
			body, err := generatedconfig.RenderHysteria2Inbound(input.Settings, input.Inbounds[0])
			return generatedconfig.GeneratedConfigArtifact{Path: input.Paths.Generated(generatedconfig.Hysteria2ConfigSubpath), Body: body}, true, err
		},
	}
}
