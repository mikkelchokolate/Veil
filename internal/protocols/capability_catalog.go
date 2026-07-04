package protocols

import "github.com/mikkelchokolate/Veil/internal/generatedconfig"

// NewGeneratedConfigRegistry builds the generatedconfig registry from plugins.
func NewGeneratedConfigRegistry() generatedconfig.ProtocolRegistry {
	return newGeneratedConfigRegistryFrom(NewRegistry())
}

func newGeneratedConfigRegistryFrom(r *Registry) generatedconfig.ProtocolRegistry {
	protocolRenderers := []generatedconfig.Protocol{}
	for _, p := range r.All() {
		cr, ok := AsConfigRenderer(p)
		if !ok {
			continue
		}
		meta := MetadataOf(p)
		protocolRenderers = append(protocolRenderers, generatedconfig.Protocol{
			Protocol:               meta.Protocol,
			MaxEnabled:             meta.MaxEnabled,
			RequiresRenderSettings: meta.Protocol != "mieru",
			Render:                 cr.RenderConfig,
		})
	}
	return generatedconfig.NewProtocolRegistry(protocolRenderers)
}

// Legacy unit constants remain here until all callers migrate to the registry.
const (
	UnitCaddy     = "veil-caddy@.service"
	UnitHysteria2 = "veil-hysteria2@.service"
	UnitOlcrtc    = "veil-olcrtc@.service"
	UnitMieru     = "veil-mieru.service"
)
