package installer

type RURecommendedInputDefaults struct{}

func NewRURecommendedInputDefaults() RURecommendedInputDefaults { return RURecommendedInputDefaults{} }

func (RURecommendedInputDefaults) Apply(input RURecommendedInput) RURecommendedInput {
	if input.Secret == nil {
		input.Secret = func(label string) string { return label }
	}
	if input.RandomPort == nil {
		input.RandomPort = func() int { return 443 }
	}
	return input
}
