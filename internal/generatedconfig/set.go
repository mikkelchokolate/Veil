package generatedconfig

type ArtifactRenderer func(Paths) (GeneratedConfigArtifact, bool, error)

type SetInput struct {
	ApplyRoot   string
	LiveRoot    string
	Settings    Settings
	Inbounds    []Inbound
	WarpConfig  WarpConfig
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
	configs, err := input.Registry.Render(ConfigInput{
		ApplyRoot: input.ApplyRoot,
		LiveRoot:  input.LiveRoot,
		Settings:  input.Settings,
		Inbounds:  input.Inbounds,
		Warp:      input.WarpConfig,
	})
	if err != nil {
		return nil, err
	}
	paths := NewPathsWithLiveRoot(input.ApplyRoot, input.LiveRoot)
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
