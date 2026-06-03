package protocols

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type Capability struct {
	Protocol               string
	DisplayName            string
	Transports             []string
	FirewallService        string
	RequiresCaddy          bool
	GeneratedConfig        generatedconfig.ArtifactSpec
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
	RenderGeneratedConfig  func(generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error)
}

type CapabilityCatalog struct {
	capabilities []Capability
}

func NewCapabilityCatalog() CapabilityCatalog {
	return CapabilityCatalog{capabilities: []Capability{
		naiveProxyCapability(),
		hysteria2Capability(),
		olcrtcCapability(),
		mieruCapability(),
	}}
}

func (c CapabilityCatalog) All() []Capability {
	out := make([]Capability, len(c.capabilities))
	for i, capability := range c.capabilities {
		capability.Transports = append([]string(nil), capability.Transports...)
		out[i] = capability
	}
	return out
}

func (c CapabilityCatalog) ForProtocol(protocol string) (Capability, bool) {
	for _, capability := range c.capabilities {
		if capability.Protocol == protocol {
			capability.Transports = append([]string(nil), capability.Transports...)
			return capability, true
		}
	}
	return Capability{}, false
}

func (c CapabilityCatalog) Choices() []Choice {
	choices := []Choice{}
	for _, capability := range c.All() {
		choices = append(choices, Choice{Protocol: capability.Protocol, DisplayName: capability.DisplayName, Transports: capability.Transports, FirewallService: capability.FirewallService, RequiresCaddy: capability.RequiresCaddy})
	}
	return choices
}

func NewGeneratedConfigRegistry() generatedconfig.ProtocolRegistry {
	protocolRenderers := []generatedconfig.Protocol{}
	for _, capability := range NewCapabilityCatalog().All() {
		if capability.RenderGeneratedConfig == nil {
			continue
		}
		protocolRenderers = append(protocolRenderers, generatedconfig.Protocol{
			Protocol:               capability.Protocol,
			MaxEnabled:             capability.MaxEnabled,
			RequiresRenderSettings: capability.RequiresRenderSettings,
			Render:                 capability.RenderGeneratedConfig,
		})
	}
	return generatedconfig.NewProtocolRegistry(protocolRenderers)
}

func naiveProxyCapability() Capability {
	return Capability{
		Protocol:               "naiveproxy",
		DisplayName:            "NaiveProxy",
		Transports:             []string{"tcp"},
		FirewallService:        "Veil NaiveProxy",
		RequiresCaddy:          true,
		GeneratedConfig:        generatedconfig.ArtifactSpec{Subpath: generatedconfig.CaddyfileSubpath, ValidationName: "caddy", ValidationCommand: func(path string) []string { return []string{"caddy", "validate", "--config", path} }},
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
		RenderGeneratedConfig: func(input generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return generatedconfig.GeneratedConfigArtifact{}, false, nil
			}
			body, err := generatedconfig.RenderNaiveInbound(input.Settings, input.Inbounds[0], input.Warp)
			return generatedconfig.GeneratedConfigArtifact{Path: input.Paths.Generated(generatedconfig.CaddyfileSubpath), Body: body}, true, err
		},
	}
}

func hysteria2Capability() Capability {
	return Capability{
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
		RenderGeneratedConfig: func(input generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return generatedconfig.GeneratedConfigArtifact{}, false, nil
			}
			body, err := generatedconfig.RenderHysteria2Inbound(input.Settings, input.Inbounds[0], input.Warp)
			return generatedconfig.GeneratedConfigArtifact{Path: input.Paths.Generated(generatedconfig.Hysteria2ConfigSubpath), Body: body}, true, err
		},
	}
}

func olcrtcCapability() Capability {
	return Capability{
		Protocol:               "olcrtc",
		DisplayName:            "olcRTC",
		Transports:             []string{"udp"},
		FirewallService:        "",
		GeneratedConfig:        generatedconfig.ArtifactSpec{Subpath: generatedconfig.OlcrtcConfigSubpath, ValidationName: "olcrtc", ValidationCommand: func(path string) []string { return []string{"olcrtc", "--config", path, "--check"} }},
		ApplyAction:            "restart " + renderer.UnitOlcrtc,
		RuntimeName:            "olcrtc",
		RuntimeActionName:      "olcrtc",
		RuntimeUnit:            renderer.UnitOlcrtc,
		RuntimeTransport:       "udp",
		RuntimeOrder:           30,
		PromotedVerb:           "restart",
		ValidateInboundRender:  true,
		RequiresRenderSettings: true,
		MaxEnabled:             1,
		RenderGeneratedConfig: func(input generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			if len(input.Inbounds) == 0 {
				return generatedconfig.GeneratedConfigArtifact{}, false, nil
			}
			body, err := generatedconfig.RenderOlcrtcInbound(input.Settings, input.Inbounds[0], input.Warp)
			return generatedconfig.GeneratedConfigArtifact{Path: input.Paths.Generated(generatedconfig.OlcrtcConfigSubpath), Body: body}, true, err
		},
	}
}

func mieruCapability() Capability {
	return Capability{
		Protocol:              "mieru",
		DisplayName:           "Mieru",
		Transports:            []string{"tcp", "udp"},
		FirewallService:       "Veil Mieru",
		GeneratedConfig:       generatedconfig.ArtifactSpec{Subpath: generatedconfig.MieruConfigSubpath, ValidationName: "mieru", ValidationCommand: func(path string) []string { return []string{"mieru", "check", "-c", path} }},
		ApplyAction:           "restart " + renderer.UnitMieru,
		RuntimeName:           "mieru",
		RuntimeActionName:     "mieru",
		RuntimeUnit:           renderer.UnitMieru,
		RuntimeOrder:          40,
		PromotedVerb:          "restart",
		ValidateInboundRender: true,
		RenderGeneratedConfig: func(input generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
			return generatedconfig.NewGeneratedMieruConfigRenderer(input.Settings, input.Paths).Render(input.Inbounds)
		},
	}
}
