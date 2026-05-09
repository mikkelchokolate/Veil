package api

import "github.com/veil-panel/veil/internal/renderer"

type Hysteria2ProtocolAdapter struct{}

func (Hysteria2ProtocolAdapter) Capability() ProtocolCapability {
	return ProtocolCapability{
		Protocol:               "hysteria2",
		DisplayName:            "Hysteria2",
		Transports:             []string{"udp"},
		FirewallService:        "Veil Hysteria2",
		GeneratedConfig:        GeneratedConfigArtifactSpec{Subpath: generatedHysteria2ConfigSubpath, ValidationName: "hysteria2", ValidationCommand: func(path string) []string { return []string{"hysteria", "server", "--config", path, "--check"} }},
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
		RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return GeneratedConfigArtifact{}, false, nil
			}
			body, err := renderHysteria2GeneratedConfig(input.Settings, input.Inbounds[0])
			return GeneratedConfigArtifact{Path: input.Paths.Generated(generatedHysteria2ConfigSubpath), Body: body}, true, err
		},
		ProfileClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
			link := newProtocolClientLink(input)
			link.URI = hysteria2UserPassClientURI(input.Settings.Domain, input.Inbound.Port, input.Credential.Username, input.Credential.Password, link.Name)
			return link, true
		},
		FallbackClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
			link := newProtocolClientLink(input)
			password := input.Inbound.Password
			if password == "" {
				password = input.Settings.Hysteria2Password
			}
			link.URI = hysteria2ClientURI(input.Settings.Domain, input.Inbound.Port, password, input.Inbound.Name)
			return link, true
		},
	}
}
