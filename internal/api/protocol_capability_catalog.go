package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type ProtocolCapability struct {
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
	RenderGeneratedConfig  func(GeneratedConfigProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error)
}

type InboundProtocolAdapter interface {
	Capability() ProtocolCapability
}

type ProtocolCapabilityCatalog struct {
	capabilities []ProtocolCapability
}

func NewProtocolCapabilityCatalog() ProtocolCapabilityCatalog {
	return NewProtocolCapabilityCatalogFromAdapters(
		NaiveProxyProtocolAdapter{},
		Hysteria2ProtocolAdapter{},
		MieruProtocolAdapter{},
	)
}

func NewProtocolCapabilityCatalogFromAdapters(adapters ...InboundProtocolAdapter) ProtocolCapabilityCatalog {
	capabilities := make([]ProtocolCapability, 0, len(adapters))
	for _, adapter := range adapters {
		capability := adapter.Capability()
		capability.Transports = append([]string(nil), capability.Transports...)
		capabilities = append(capabilities, capability)
	}
	return ProtocolCapabilityCatalog{capabilities: capabilities}
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
