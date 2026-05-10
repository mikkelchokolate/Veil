package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/renderer"
)

type MieruProtocolAdapter struct{}

func (MieruProtocolAdapter) Capability() ProtocolCapability {
	return ProtocolCapability{
		Protocol:              "mieru",
		DisplayName:           "Mieru",
		Transports:            []string{"tcp", "udp"},
		FirewallService:       "Veil Mieru",
		GeneratedConfig:       GeneratedConfigArtifactSpec{Subpath: generatedMieruConfigSubpath, ValidationName: "mieru", ValidationCommand: func(path string) []string { return []string{"mieru", "check", "-c", path} }},
		ApplyAction:           "restart " + renderer.UnitMieru,
		RuntimeName:           "mieru",
		RuntimeActionName:     "mieru",
		RuntimeUnit:           renderer.UnitMieru,
		RuntimeOrder:          40,
		PromotedVerb:          "restart",
		ValidateInboundRender: true,
		RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
			return generatedconfig.NewGeneratedMieruConfigRenderer(input.Settings, input.Paths).Render(input.Inbounds)
		},
		ProfileClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
			return mieruClientConfigLink(input)
		},
		FallbackClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
			input.Credential = ClientCredential{Name: input.Inbound.Name, Username: input.Inbound.Name, Password: input.Inbound.Password}
			return mieruClientConfigLink(input)
		},
		AggregateClientLinks: func(settings Settings, inbounds []Inbound) ([]ClientLink, error) {
			return NewMieruClientAccessAggregator().Build(settings, inbounds)
		},
	}
}
