package api

import "github.com/veil-panel/veil/internal/renderer"

type ProtocolCapability struct {
	Protocol               string
	DisplayName            string
	Transports             []string
	FirewallService        string
	RequiresCaddy          bool
	GeneratedConfig        GeneratedConfigArtifactSpec
	ApplyAction            string
	RuntimeName            string
	RuntimeActionName      string
	RuntimeUnit            string
	RuntimeTransport       string
	RuntimeOrder           int
	PromotedVerb           string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
	RequiresCaddySettings  bool
	MaxEnabled             int
	RenderGeneratedConfig  func(GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error)
	ProfileClientLink      func(ClientAccessLinkInput) (ClientLink, bool)
	FallbackClientLink     func(ClientAccessLinkInput) (ClientLink, bool)
	AggregateClientLinks   func(Settings, []Inbound) ([]ClientLink, error)
}

type ProtocolCapabilityCatalog struct {
	capabilities []ProtocolCapability
}

func NewProtocolCapabilityCatalog() ProtocolCapabilityCatalog {
	return ProtocolCapabilityCatalog{capabilities: []ProtocolCapability{
		{
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
			ProfileClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				link.URI = naiveClientURI(input.Settings.Domain, input.Inbound.Port, input.Credential.Username, input.Credential.Password)
				return link, true
			},
			FallbackClientLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				password := input.Inbound.Password
				if password == "" {
					password = input.Settings.NaivePassword
				}
				link.URI = naiveClientURI(input.Settings.Domain, input.Inbound.Port, input.Settings.NaiveUsername, password)
				return link, true
			},
		},
		{
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
		},
		{
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
				return NewGeneratedMieruConfigRenderer(input.Settings, input.Paths).Render(input.Inbounds)
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
		},
	}}
}

func (c ProtocolCapabilityCatalog) All() []ProtocolCapability {
	out := make([]ProtocolCapability, len(c.capabilities))
	for i, capability := range c.capabilities {
		capability.Transports = append([]string(nil), capability.Transports...)
		out[i] = capability
	}
	return out
}

func (c ProtocolCapabilityCatalog) ForProtocol(protocol string) (ProtocolCapability, bool) {
	for _, capability := range c.capabilities {
		if capability.Protocol == protocol {
			capability.Transports = append([]string(nil), capability.Transports...)
			return capability, true
		}
	}
	return ProtocolCapability{}, false
}
