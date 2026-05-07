package installer

import "github.com/veil-panel/veil/internal/renderer"

type RURecommendedNaiveArtifacts struct{}

func NewRURecommendedNaiveArtifacts() RURecommendedNaiveArtifacts {
	return RURecommendedNaiveArtifacts{}
}

func (RURecommendedNaiveArtifacts) Build(input RURecommendedInput, plan SharedPortPlan, defaults RURecommendedDefaults, webBasePath string) (ruRecommendedNaiveArtifacts, error) {
	password := input.Secret("naive")
	caddyfile, err := renderer.RenderNaiveCaddyfile(renderer.NaiveConfig{
		Domain:       input.Domain,
		Email:        input.Email,
		ListenPort:   plan.Port,
		Username:     defaults.Username,
		Password:     password,
		FallbackRoot: defaults.FallbackRoot,
		PanelPort:    input.PanelPort,
		WebBasePath:  webBasePath,
	})
	if err != nil {
		return ruRecommendedNaiveArtifacts{}, err
	}
	return ruRecommendedNaiveArtifacts{
		Password:  password,
		Caddyfile: caddyfile,
		ClientURL: naiveURL(defaults.Username, password, input.Domain, plan.Port),
	}, nil
}
