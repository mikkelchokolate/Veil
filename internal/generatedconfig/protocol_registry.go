package generatedconfig

import "fmt"

type ConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Warp      WarpConfig
}

type ProtocolRegistry struct {
	protocols []Protocol
}

type Protocol struct {
	Protocol               string
	MaxEnabled             int
	RequiresRenderSettings bool
	Render                 func(ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error)
	ArtifactSpec           ArtifactSpec
}

type ProtocolRenderInput struct {
	Settings Settings
	Paths    Paths
	Inbounds []Inbound
	Warp     WarpConfig
}

func NewProtocolRegistry(protocols []Protocol) ProtocolRegistry {
	out := make([]Protocol, len(protocols))
	copy(out, protocols)
	return ProtocolRegistry{protocols: out}
}

func (r ProtocolRegistry) Validate(settings Settings, inbounds []Inbound) error {
	for _, protocol := range r.protocols {
		if protocol.MaxEnabled <= 0 {
			continue
		}
		count := len(r.enabledInbounds(settings, inbounds, protocol.Protocol))
		if count > protocol.MaxEnabled {
			return fmt.Errorf("multiple enabled %s inbounds are not renderable as a single generated config yet", protocol.Protocol)
		}
	}
	return nil
}

func (r ProtocolRegistry) Render(input ConfigInput) (map[string]string, error) {
	if err := r.Validate(input.Settings, input.Inbounds); err != nil {
		return nil, err
	}
	paths := NewPaths(input.ApplyRoot)
	configs := map[string]string{}
	for _, protocol := range r.protocols {
		selected := r.enabledInbounds(input.Settings, input.Inbounds, protocol.Protocol)
		if len(selected) == 0 {
			continue
		}
		if protocol.RequiresRenderSettings && !NewGeneratedRenderSettingsPolicy().HasRenderSettings(input.Settings, input.Inbounds) {
			continue
		}
		artifacts, ok, err := protocol.Render(ProtocolRenderInput{Settings: input.Settings, Paths: paths, Inbounds: selected, Warp: input.Warp})
		if err != nil {
			return nil, err
		}
		if ok {
			for _, artifact := range artifacts {
				configs[artifact.Path] = artifact.Body
			}
		}
	}
	return configs, nil
}

func (r ProtocolRegistry) RenderInbound(settings Settings, paths Paths, inbound Inbound, warp WarpConfig) (GeneratedConfigArtifact, bool, error) {
	protocol, ok := r.protocol(inbound.Protocol)
	if !ok {
		return GeneratedConfigArtifact{}, false, nil
	}
	arts, ok, err := protocol.Render(ProtocolRenderInput{Settings: settings, Paths: paths, Inbounds: []Inbound{inbound}, Warp: warp})
	if err != nil || !ok || len(arts) == 0 {
		return GeneratedConfigArtifact{}, ok, err
	}
	return arts[0], true, nil
}

func (r ProtocolRegistry) protocol(protocol string) (Protocol, bool) {
	for _, candidate := range r.protocols {
		if candidate.Protocol == protocol {
			return candidate, true
		}
	}
	return Protocol{}, false
}

func (ProtocolRegistry) enabledInbounds(settings Settings, inbounds []Inbound, protocol string) []Inbound {
	selected := []Inbound{}
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == protocol {
			selected = append(selected, inbound)
		}
	}
	return selected
}

// ArtifactSpecs returns the artifact metadata for every registered protocol that
// contributes generated config artifacts. The result preserves registration order.
func (r ProtocolRegistry) ArtifactSpecs() []ArtifactSpec {
	out := make([]ArtifactSpec, 0, len(r.protocols))
	for _, protocol := range r.protocols {
		if protocol.ArtifactSpec.Subpath == "" {
			continue
		}
		out = append(out, protocol.ArtifactSpec)
	}
	return out
}
