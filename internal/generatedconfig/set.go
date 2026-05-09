package generatedconfig

type ArtifactRenderer func(Paths) (GeneratedConfigArtifact, bool, error)

type SetInput struct {
	ApplyRoot   string
	Settings    Settings
	Inbounds    []Inbound
	Registry    ProtocolRegistry
	PanelAccess ArtifactRenderer
	Warp        ArtifactRenderer
}

type SetBuilder struct {
	input SetInput
}

func NewSetBuilder(input SetInput) SetBuilder {
	return SetBuilder{input: input}
}

func (b SetBuilder) Build() (map[string]string, error) {
	input := b.input
	configs, err := input.Registry.Render(ConfigInput{ApplyRoot: input.ApplyRoot, Settings: input.Settings, Inbounds: input.Inbounds})
	if err != nil {
		return nil, err
	}
	paths := NewPaths(input.ApplyRoot)
	if _, exists := configs[paths.Caddyfile()]; !exists && input.PanelAccess != nil {
		artifact, ok, err := input.PanelAccess(paths)
		if err != nil {
			return nil, err
		}
		if ok {
			configs[artifact.Path] = artifact.Body
		}
	}
	if input.Warp != nil {
		artifact, ok, err := input.Warp(paths)
		if err != nil {
			return nil, err
		}
		if ok {
			configs[artifact.Path] = artifact.Body
		}
	}
	return configs, nil
}
