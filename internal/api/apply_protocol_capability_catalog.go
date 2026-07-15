package api

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type ApplyProtocolCapability struct {
	Protocol               string
	Config                 string
	Action                 string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
}

type ApplyProtocolCapabilityCatalog struct {
	byProtocol map[string]ApplyProtocolCapability
}

func NewApplyProtocolCapabilityCatalog() ApplyProtocolCapabilityCatalog {
	byProtocol := map[string]ApplyProtocolCapability{}
	registry := protocols.NewRegistry()
	requiresCaddy := protocols.NewCatalog().RequiresCaddy
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
				if descs[0].PromotedVerb != "" {
					cap.Action = descs[0].PromotedVerb + " " + unit
				}
			}
		}
		// Caddy-managed protocols render one global Caddy JSON artifact and
		// reload the consolidated veil-caddy.service. Per-protocol entries would
		// duplicate that global material and can produce contradictory plans.
		if requiresCaddy(meta.Protocol) {
			cap.Config = ""
			cap.Action = ""
		}
		byProtocol[meta.Protocol] = cap
	}
	return ApplyProtocolCapabilityCatalog{byProtocol: byProtocol}
}

func (c ApplyProtocolCapabilityCatalog) Capability(protocol string) (ApplyProtocolCapability, bool) {
	capability, ok := c.byProtocol[protocol]
	return capability, ok
}

// ForProtocol is the compatibility lookup used by the apply-plan builder.
func (c ApplyProtocolCapabilityCatalog) ForProtocol(protocol string) (ApplyProtocolCapability, bool) {
	return c.Capability(protocol)
}

func (c ApplyProtocolCapabilityCatalog) All() []ApplyProtocolCapability {
	capabilities := make([]ApplyProtocolCapability, 0, len(c.byProtocol))
	for _, capability := range c.byProtocol {
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

func (c ApplyProtocolCapability) ValidateSettings(settings Settings, inbound Inbound) error {
	return validateProtocolSettings(c.Protocol, settings, inbound)
}

func validateProtocolSettings(protocol string, settings Settings, inbound Inbound) error {
	p, ok := protocols.NewRegistry().Get(protocol)
	if !ok {
		return nil
	}
	validator, ok := protocols.AsValidator(p)
	if !ok {
		return nil
	}
	return validator.ValidateSettings(settings, inbound)
}

func (c ApplyProtocolCapability) ValidateInbound(settings Settings, inbound Inbound) []model.ValidationIssue {
	return validateProtocolInbound(c.Protocol, settings, inbound)
}

func validateProtocolInbound(protocol string, settings Settings, inbound Inbound) []model.ValidationIssue {
	p, ok := protocols.NewRegistry().Get(protocol)
	if !ok {
		return nil
	}
	validator, ok := protocols.AsValidator(p)
	if !ok {
		return nil
	}
	return validator.ValidateInbound(settings, inbound)
}

func (c ApplyProtocolCapability) ShouldValidateRender(renderSettingsAvailable bool) bool {
	return c.ValidateInboundRender && (!c.RequiresRenderSettings || renderSettingsAvailable)
}
