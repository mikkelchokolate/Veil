package installer

import "github.com/veil-panel/veil/internal/renderer"

type RURecommendedHysteriaArtifacts struct{}

func NewRURecommendedHysteriaArtifacts() RURecommendedHysteriaArtifacts {
	return RURecommendedHysteriaArtifacts{}
}

func (RURecommendedHysteriaArtifacts) Build(input RURecommendedInput, plan SharedPortPlan, defaults RURecommendedDefaults) (ruRecommendedHysteriaArtifacts, error) {
	password := input.Secret("hysteria2")
	yaml, err := renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:    plan.Port,
		Domain:        input.Domain,
		Password:      password,
		MasqueradeURL: defaults.MasqueradeURL,
	})
	if err != nil {
		return ruRecommendedHysteriaArtifacts{}, err
	}
	return ruRecommendedHysteriaArtifacts{
		Password:   password,
		ServerYAML: yaml,
		ClientURI:  hysteria2URI(password, input.Domain, plan.Port),
	}, nil
}
