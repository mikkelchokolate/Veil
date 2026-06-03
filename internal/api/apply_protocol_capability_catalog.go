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
	for _, capability := range protocols.NewCapabilityCatalog().All() {
		byProtocol[capability.Protocol] = ApplyProtocolCapability{
			Protocol:               capability.Protocol,
			Config:                 capability.GeneratedConfig.PlanPath(),
			Action:                 capability.ApplyAction,
			ValidateInboundRender:  capability.ValidateInboundRender,
			RequiresRenderSettings: capability.RequiresRenderSettings,
			RequiresCaddySettings:  capability.RequiresCaddySettings,
		}
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
