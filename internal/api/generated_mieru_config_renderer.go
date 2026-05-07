package api

import "github.com/veil-panel/veil/internal/renderer"

type GeneratedMieruConfigRenderer struct {
	settings Settings
	paths    GeneratedConfigPaths
}

func NewGeneratedMieruConfigRenderer(settings Settings, paths GeneratedConfigPaths) GeneratedMieruConfigRenderer {
	return GeneratedMieruConfigRenderer{settings: settings, paths: paths}
}

func (r GeneratedMieruConfigRenderer) Render(inbounds []Inbound) (GeneratedConfigArtifact, bool, error) {
	bindings := []renderer.MieruPortBinding{}
	users := []renderer.MieruUser{}
	for _, inbound := range inbounds {
		if !inbound.Enabled || inbound.Protocol != "mieru" || !stackIncludesProtocol(r.settings.Stack, inbound.Protocol) {
			continue
		}
		bindings = append(bindings, renderer.MieruPortBinding{Port: inbound.Port, Protocol: inbound.Transport})
		credentials, err := BuildClientCredentials(inbound)
		if err != nil {
			return GeneratedConfigArtifact{}, false, err
		}
		if len(credentials) == 0 {
			users = append(users, renderer.MieruUser{Name: inbound.Name, Password: inbound.Password})
			continue
		}
		for _, credential := range credentials {
			users = append(users, renderer.MieruUser{Name: credential.Username, Password: credential.Password})
		}
	}
	if len(bindings) == 0 {
		return GeneratedConfigArtifact{}, false, nil
	}
	body, err := renderer.RenderMieru(renderer.MieruConfig{PortBindings: bindings, Users: users})
	return GeneratedConfigArtifact{Path: r.paths.Mieru(), Body: body}, true, err
}
