package mieru

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
)

// RenderConfig generates the aggregated mieru server config.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}
	art, ok, err := generatedconfig.NewGeneratedMieruConfigRenderer(input.Settings, input.Paths).Render(input.Inbounds)
	if err != nil || !ok {
		return nil, ok, err
	}
	return []generatedconfig.GeneratedConfigArtifact{art}, true, nil
}

// ArtifactSpec returns the artifact metadata for the mieru config.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.MieruConfigSubpath,
		ValidationName: "mieru",
		ValidationCommand: func(path string) []string {
			return []string{"mieru", "check", "-c", path}
		},
	}
}
