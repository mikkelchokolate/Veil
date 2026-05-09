package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigProtocolRegistry struct {
	inner generatedconfig.ProtocolRegistry
}

type GeneratedConfigProtocol struct {
	Protocol               string
	MaxEnabled             int
	RequiresRenderSettings bool
	Render                 func(GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error)
}

type GeneratedConfigProtocolRenderInput struct {
	Settings Settings
	Paths    GeneratedConfigPaths
	Inbounds []Inbound
}

func NewGeneratedConfigProtocolRegistry() GeneratedConfigProtocolRegistry {
	protocols := []generatedconfig.Protocol{}
	for _, capability := range NewProtocolCapabilityCatalog().All() {
		if capability.RenderGeneratedConfig == nil {
			continue
		}
		render := capability.RenderGeneratedConfig
		protocols = append(protocols, generatedconfig.Protocol{
			Protocol:               capability.Protocol,
			MaxEnabled:             capability.MaxEnabled,
			RequiresRenderSettings: capability.RequiresRenderSettings,
			Render: func(input generatedconfig.ProtocolRenderInput) (generatedconfig.GeneratedConfigArtifact, bool, error) {
				return render(GeneratedConfigProtocolRenderInput{Settings: input.Settings, Paths: input.Paths, Inbounds: input.Inbounds})
			},
		})
	}
	return GeneratedConfigProtocolRegistry{inner: generatedconfig.NewProtocolRegistry(protocols)}
}

func (r GeneratedConfigProtocolRegistry) Validate(settings Settings, inbounds []Inbound) error {
	return r.inner.Validate(settings, inbounds)
}

func (r GeneratedConfigProtocolRegistry) Render(input GeneratedConfigInput) (map[string]string, error) {
	return r.inner.Render(generatedconfig.ConfigInput{ApplyRoot: input.ApplyRoot, Settings: input.Settings, Inbounds: input.Inbounds})
}

func (r GeneratedConfigProtocolRegistry) RenderInbound(settings Settings, paths GeneratedConfigPaths, inbound Inbound) (GeneratedConfigArtifact, bool, error) {
	return r.inner.RenderInbound(settings, paths, inbound)
}
