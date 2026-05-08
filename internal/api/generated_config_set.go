package api

type GeneratedConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	configs, err := NewGeneratedConfigProtocolRegistry().Render(input)
	if err != nil {
		return nil, err
	}
	paths := NewGeneratedConfigPaths(input.ApplyRoot)
	artifact, ok, err := NewGeneratedWarpConfigRenderer(paths).Render(input.Warp, input.Rules)
	if err != nil {
		return nil, err
	}
	if ok {
		configs[artifact.Path] = artifact.Body
	}
	return configs, nil
}
