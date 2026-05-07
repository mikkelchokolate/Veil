package api

type GeneratedConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	if err := validateGeneratedConfigInboundCardinality(input.Settings, input.Inbounds); err != nil {
		return nil, err
	}
	configs := map[string]string{}
	paths := NewGeneratedConfigPaths(input.ApplyRoot)
	if hasRenderSettings(input.Settings) {
		renderer := NewGeneratedInboundConfigRenderer(input.Settings, paths)
		for _, inbound := range input.Inbounds {
			artifact, ok, err := renderer.Render(inbound)
			if err != nil {
				return nil, err
			}
			if ok {
				configs[artifact.Path] = artifact.Body
			}
		}
	}
	artifact, ok, err := NewGeneratedMieruConfigRenderer(input.Settings, paths).Render(input.Inbounds)
	if err != nil {
		return nil, err
	}
	if ok {
		configs[artifact.Path] = artifact.Body
	}
	artifact, ok, err = NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
	if err != nil {
		return nil, err
	}
	if ok {
		configs[artifact.Path] = artifact.Body
	}
	return configs, nil
}
