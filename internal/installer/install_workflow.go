package installer

// RURecommendedInstallInput contains the decisions and adapters needed to build
// a ru-recommended install profile plus its selected panel port.
type RURecommendedInstallInput struct {
	Domain          string
	Email           string
	Stack           Stack
	Port            int
	PanelPort       int
	Availability    PortAvailability
	Secret          SecretFunc
	RandomPort      func() int
	RandomPanelPort func() (int, error)
}

type RURecommendedInstall struct {
	Profile     RURecommendedProfile
	PanelPort   int
	PanelRandom bool
}

// BuildRURecommendedInstall concentrates the ordering invariant that the panel
// port must be selected before profile rendering, because the Caddyfile embeds
// that port for the Panel reverse proxy.
func BuildRURecommendedInstall(input RURecommendedInstallInput) (RURecommendedInstall, error) {
	randomPanelPort := input.RandomPanelPort
	if randomPanelPort == nil {
		randomPanelPort = RandomHighPort
	}
	panelPort, panelRandom, err := SelectPanelPort(input.PanelPort, randomPanelPort)
	if err != nil {
		return RURecommendedInstall{}, err
	}
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       input.Domain,
		Email:        input.Email,
		Stack:        input.Stack,
		Port:         input.Port,
		Availability: input.Availability,
		Secret:       input.Secret,
		RandomPort:   input.RandomPort,
		PanelPort:    panelPort,
	})
	if err != nil {
		return RURecommendedInstall{}, err
	}
	return RURecommendedInstall{Profile: profile, PanelPort: panelPort, PanelRandom: panelRandom}, nil
}
