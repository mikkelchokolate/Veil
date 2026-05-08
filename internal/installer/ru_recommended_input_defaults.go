package installer

type RURecommendedInputDefaults struct{}

func NewRURecommendedInputDefaults() RURecommendedInputDefaults { return RURecommendedInputDefaults{} }

func (RURecommendedInputDefaults) Apply(input RURecommendedInput) RURecommendedInput {
	if input.PanelAccess == "" {
		input.PanelAccess = "local"
	}
	if input.Secret == nil {
		input.Secret = func(label string) string { return label }
	}
	return input
}
