package api

type ProtocolCapability struct {
	Protocol               string
	DisplayName            string
	Transports             []string
	FirewallService        string
	RequiresCaddy          bool
	GeneratedConfigPath    string
	GeneratedConfigSuffix  string
	ApplyAction            string
	RuntimeName            string
	RuntimeActionName      string
	RuntimeUnit            string
	RuntimeTransport       string
	RuntimeOrder           int
	PromotedSubpath        string
	PromotedVerb           string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
	RequiresCaddySettings  bool
	ValidationName         string
	ValidationCommand      func(string) []string
	MaxEnabled             int
	RenderGeneratedConfig  func(GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error)
	ProfileClientLink      func(ClientAccessLinkInput) (ClientLink, bool)
	FallbackClientLink     func(ClientAccessLinkInput) (ClientLink, bool)
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
			GeneratedConfigPath:    "/etc/veil/generated/caddy/Caddyfile",
			GeneratedConfigSuffix:  "/generated/caddy/Caddyfile",
			ApplyAction:            "reload veil-naive.service",
			RuntimeName:            "naive",
			RuntimeActionName:      "caddy",
			RuntimeUnit:            "veil-naive.service",
			RuntimeTransport:       "tcp",
			RuntimeOrder:           10,
			PromotedSubpath:        "caddy/Caddyfile",
			PromotedVerb:           "reload",
			ValidateInboundRender:  true,
			RequiresRenderSettings: true,
			RequiresCaddySettings:  true,
			ValidationName:         "caddy",
			ValidationCommand:      func(path string) []string { return []string{"caddy", "validate", "--config", path} },
			MaxEnabled:             1,
			RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
				if len(input.Inbounds) == 0 {
					return GeneratedConfigArtifact{}, false, nil
				}
				body, err := renderNaiveGeneratedConfig(input.Settings, input.Inbounds[0])
				return GeneratedConfigArtifact{Path: input.Paths.Caddyfile(), Body: body}, true, err
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
			GeneratedConfigPath:    "/etc/veil/generated/hysteria2/server.yaml",
			GeneratedConfigSuffix:  "/generated/hysteria2/server.yaml",
			ApplyAction:            "reload veil-hysteria2.service",
			RuntimeName:            "hysteria2",
			RuntimeActionName:      "hysteria2",
			RuntimeUnit:            "veil-hysteria2.service",
			RuntimeTransport:       "udp",
			RuntimeOrder:           20,
			PromotedSubpath:        "hysteria2/server.yaml",
			PromotedVerb:           "reload",
			ValidateInboundRender:  true,
			RequiresRenderSettings: true,
			ValidationName:         "hysteria2",
			ValidationCommand:      func(path string) []string { return []string{"hysteria", "server", "--config", path, "--check"} },
			MaxEnabled:             1,
			RenderGeneratedConfig: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
				if len(input.Inbounds) == 0 {
					return GeneratedConfigArtifact{}, false, nil
				}
				body, err := renderHysteria2GeneratedConfig(input.Settings, input.Inbounds[0])
				return GeneratedConfigArtifact{Path: input.Paths.Hysteria2(), Body: body}, true, err
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
			GeneratedConfigPath:   "/etc/veil/generated/mieru/server_config.json",
			GeneratedConfigSuffix: "/generated/mieru/server_config.json",
			ApplyAction:           "restart veil-mieru.service",
			RuntimeName:           "mieru",
			RuntimeActionName:     "mieru",
			RuntimeUnit:           "veil-mieru.service",
			RuntimeOrder:          40,
			PromotedSubpath:       "mieru/server_config.json",
			PromotedVerb:          "restart",
			ValidateInboundRender: true,
			ValidationName:        "mieru",
			ValidationCommand:     func(path string) []string { return []string{"mieru", "check", "-c", path} },
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
