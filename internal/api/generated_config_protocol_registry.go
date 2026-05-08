package api

import "fmt"

type GeneratedConfigProtocolRegistry struct {
	protocols []GeneratedConfigProtocol
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
	return GeneratedConfigProtocolRegistry{protocols: []GeneratedConfigProtocol{
		{
			Protocol:               "naiveproxy",
			MaxEnabled:             1,
			RequiresRenderSettings: true,
			Render: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
				if len(input.Inbounds) == 0 {
					return GeneratedConfigArtifact{}, false, nil
				}
				body, err := renderNaiveGeneratedConfig(input.Settings, input.Inbounds[0])
				return GeneratedConfigArtifact{Path: input.Paths.Caddyfile(), Body: body}, true, err
			},
		},
		{
			Protocol:               "hysteria2",
			MaxEnabled:             1,
			RequiresRenderSettings: true,
			Render: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
				if len(input.Inbounds) == 0 {
					return GeneratedConfigArtifact{}, false, nil
				}
				body, err := renderHysteria2GeneratedConfig(input.Settings, input.Inbounds[0])
				return GeneratedConfigArtifact{Path: input.Paths.Hysteria2(), Body: body}, true, err
			},
		},
		{
			Protocol: "mieru",
			Render: func(input GeneratedConfigProtocolRenderInput) (GeneratedConfigArtifact, bool, error) {
				return NewGeneratedMieruConfigRenderer(input.Settings, input.Paths).Render(input.Inbounds)
			},
		},
	}}
}

func (r GeneratedConfigProtocolRegistry) Validate(settings Settings, inbounds []Inbound) error {
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

func (r GeneratedConfigProtocolRegistry) Render(input GeneratedConfigInput) (map[string]string, error) {
	if err := r.Validate(input.Settings, input.Inbounds); err != nil {
		return nil, err
	}
	paths := NewGeneratedConfigPaths(input.ApplyRoot)
	configs := map[string]string{}
	for _, protocol := range r.protocols {
		selected := r.enabledInbounds(input.Settings, input.Inbounds, protocol.Protocol)
		if len(selected) == 0 {
			continue
		}
		if protocol.RequiresRenderSettings && !hasRenderSettings(input.Settings) {
			continue
		}
		artifact, ok, err := protocol.Render(GeneratedConfigProtocolRenderInput{Settings: input.Settings, Paths: paths, Inbounds: selected})
		if err != nil {
			return nil, err
		}
		if ok {
			configs[artifact.Path] = artifact.Body
		}
	}
	return configs, nil
}

func (r GeneratedConfigProtocolRegistry) RenderInbound(settings Settings, paths GeneratedConfigPaths, inbound Inbound) (GeneratedConfigArtifact, bool, error) {
	protocol, ok := r.protocol(inbound.Protocol)
	if !ok {
		return GeneratedConfigArtifact{}, false, nil
	}
	return protocol.Render(GeneratedConfigProtocolRenderInput{Settings: settings, Paths: paths, Inbounds: []Inbound{inbound}})
}

func (r GeneratedConfigProtocolRegistry) protocol(protocol string) (GeneratedConfigProtocol, bool) {
	for _, candidate := range r.protocols {
		if candidate.Protocol == protocol {
			return candidate, true
		}
	}
	return GeneratedConfigProtocol{}, false
}

func (GeneratedConfigProtocolRegistry) enabledInbounds(settings Settings, inbounds []Inbound, protocol string) []Inbound {
	selected := []Inbound{}
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == protocol && stackIncludesProtocol(settings.Stack, inbound.Protocol) {
			selected = append(selected, inbound)
		}
	}
	return selected
}
