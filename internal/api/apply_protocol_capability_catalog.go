package api

import (
	"github.com/mikkelchokolate/Veil/internal/panelaccess"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type ApplyProtocolCapability struct {
	Protocol               string
	Config                 string
	Action                 string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
	RequiresCaddySettings  bool
}

type ApplyProtocolCapabilityCatalog struct {
	byProtocol map[string]ApplyProtocolCapability
}

func NewApplyProtocolCapabilityCatalog() ApplyProtocolCapabilityCatalog {
	byProtocol := map[string]ApplyProtocolCapability{}
	registry := protocols.NewRegistry()
	for _, p := range registry.All() {
		meta := protocols.MetadataOf(p)
		cap := ApplyProtocolCapability{
			Protocol: meta.Protocol,
		}
		if cr, ok := protocols.AsConfigRenderer(p); ok {
			cap.Config = cr.ArtifactSpec().PlanPath()
			cap.ValidateInboundRender = true
			cap.RequiresRenderSettings = requiresProtocolRenderSettings(p)
		}
		if rp, ok := protocols.AsRuntimeProvider(p); ok {
			descs := rp.RuntimeDescriptors(nil)
			if len(descs) > 0 {
				unit := descs[0].Unit
				if descs[0].TemplateUnit != "" {
					unit = descs[0].TemplateUnit
				}
				cap.Action = "restart " + unit
			}
		}
		if _, ok := protocols.AsValidator(p); ok {
			cap.RequiresCaddySettings = meta.Protocol == "naiveproxy"
		}
		byProtocol[meta.Protocol] = cap
	}
	return ApplyProtocolCapabilityCatalog{byProtocol: byProtocol}
}

func (c ApplyProtocolCapabilityCatalog) ForProtocol(protocol string) (ApplyProtocolCapability, bool) {
	capability, ok := c.byProtocol[protocol]
	return capability, ok
}

func (c ApplyProtocolCapabilityCatalog) All() []ApplyProtocolCapability {
	capabilities := make([]ApplyProtocolCapability, 0, len(c.byProtocol))
	for _, capability := range c.byProtocol {
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

func (c ApplyProtocolCapability) ValidateSettings(settings Settings) error {
	if c.RequiresCaddySettings {
		return panelaccess.NewNaiveCaddySettingsRequirement().Validate(settings)
	}
	return nil
}

func (c ApplyProtocolCapability) ShouldValidateRender(renderSettingsAvailable bool) bool {
	return c.ValidateInboundRender && (!c.RequiresRenderSettings || renderSettingsAvailable)
}
