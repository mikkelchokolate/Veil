package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/protocols"
)

type GeneratedConfigProtocolRegistry struct {
	inner generatedconfig.ProtocolRegistry
}

func NewGeneratedConfigProtocolRegistry() GeneratedConfigProtocolRegistry {
	protocolRenderers := []generatedconfig.Protocol{}
	for _, capability := range protocols.NewCapabilityCatalog().All() {
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
	return GeneratedConfigProtocolRegistry{inner: generatedconfig.NewProtocolRegistry(protocolRenderers)}
}

func (r GeneratedConfigProtocolRegistry) Validate(settings Settings, inbounds []Inbound) error {
	return r.inner.Validate(settings, inbounds)
}

func (r GeneratedConfigProtocolRegistry) Render(input GeneratedConfigInput) (map[string]string, error) {
	return r.inner.Render(generatedconfig.ConfigInput{ApplyRoot: input.ApplyRoot, Settings: input.Settings, Inbounds: input.Inbounds})
}

func (r GeneratedConfigProtocolRegistry) RenderInbound(settings Settings, paths generatedconfig.Paths, inbound Inbound) (generatedconfig.GeneratedConfigArtifact, bool, error) {
	return r.inner.RenderInbound(settings, paths, inbound)
}
