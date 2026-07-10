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
			RequiresRenderSettings: RequiresRenderSettings(p),
			Render:                 cr.RenderConfig,
			ArtifactSpec:           cr.ArtifactSpec(),
		})
	}
	return generatedconfig.NewProtocolRegistry(protocolRenderers)
}

// NeedsCaddyCertSync reports whether a protocol requires Caddy-managed TLS
// certificates to be synced to disk when PanelAccess is "caddy".
func NeedsCaddyCertSync(protocol string) bool {
	registry := NewRegistry()
	p, ok := registry.Get(protocol)
	if !ok {
		return false
	}
	csp, ok := p.(interface{ NeedsCaddyCertSync() bool })
	return ok && csp.NeedsCaddyCertSync()
}

// Legacy unit constants remain here until all callers migrate to the registry.
const (
	UnitCaddy     = "veil-caddy@.service"
	UnitHysteria2 = "veil-hysteria2@.service"
	UnitOlcrtc    = "veil-olcrtc@.service"
	UnitMieru     = "veil-mieru.service"
)
