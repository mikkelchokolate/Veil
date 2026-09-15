package generatedconfig

import "github.com/mikkelchokolate/Veil/internal/renderer"

type GeneratedMieruConfigRenderer struct {
	settings Settings
	paths    GeneratedConfigPaths
}

func NewGeneratedMieruConfigRenderer(settings Settings, paths GeneratedConfigPaths) GeneratedMieruConfigRenderer {
	return GeneratedMieruConfigRenderer{settings: settings, paths: paths}
}

func (r GeneratedMieruConfigRenderer) Render(inbounds []Inbound) (GeneratedConfigArtifact, bool, error) {
	config, ok, err := NewMieruGeneratedConfigModel(r.settings).Build(inbounds)
	if err != nil || !ok {
		return GeneratedConfigArtifact{}, ok, err
	}
	if len(config.Users) == 0 {
		// Isolated all-disabled inbounds still contribute a port binding with
		// no users. Skip writing server_config.json instead of failing apply
		// with "at least one mieru user is required".
		return GeneratedConfigArtifact{}, false, nil
	}
	body, err := renderer.RenderMieru(config)
	return GeneratedConfigArtifact{Path: r.paths.Mieru(), Body: body}, true, err
}
