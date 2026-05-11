package installer

import "github.com/veil-panel/veil/internal/panelaccess"

type SecretFunc func(label string) string

type RURecommendedInput struct {
	Domain      string
	Email       string
	PanelAccess string
	Secret      SecretFunc
	PanelPort   int
}

type RURecommendedProfile struct {
	Domain            string
	Email             string
	Username          string
	PanelAuthToken    string
	PanelListen       string
	PanelAccess       string
	PanelTLSEnabled   bool
	PanelTLSCertPEM   string
	PanelTLSKeyPEM    string
	WebBasePath       string
	InstallPanelCaddy bool
	Caddyfile         string
	MasqueradeURL     string
	FallbackRoot      string
}

// RURecommendedInstallInput contains the decisions and adapters needed to build
// a ru-recommended install profile plus its selected panel port.
type RURecommendedInstallInput struct {
	Domain          string
	Email           string
	PanelAccess     string
	PanelPort       int
	Secret          SecretFunc
	RandomPanelPort func() (int, error)
}

type RURecommendedInstall struct {
	Profile     RURecommendedProfile
	PanelPort   int
	PanelRandom bool
}

type RURecommendedProfileModule struct {
	input RURecommendedInput
}

func NewRURecommendedProfileModule(input RURecommendedInput) RURecommendedProfileModule {
	return RURecommendedProfileModule{input: input}
}

func BuildRURecommendedProfile(input RURecommendedInput) (RURecommendedProfile, error) {
	return NewRURecommendedProfileModule(input).Build()
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
		Domain:      input.Domain,
		Email:       input.Email,
		PanelAccess: input.PanelAccess,
		Secret:      input.Secret,
		PanelPort:   panelPort,
	})
	if err != nil {
		return RURecommendedInstall{}, err
	}
	return RURecommendedInstall{Profile: profile, PanelPort: panelPort, PanelRandom: panelRandom}, nil
}

func (m RURecommendedProfileModule) Build() (RURecommendedProfile, error) {
	input := m.normalizedInput()
	username := "veil"
	masqueradeURL := "https://www.bing.com/"
	fallbackRoot := "/var/lib/veil/www"
	panelAccess, err := panelaccess.NewProfile(panelaccess.ProfileInput{PanelAccess: input.PanelAccess, Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort}).Build()
	if err != nil {
		return RURecommendedProfile{}, err
	}
	panelAuthToken := input.Secret("panel")

	return RURecommendedProfile{
		Domain:            input.Domain,
		Email:             input.Email,
		Username:          username,
		PanelAuthToken:    panelAuthToken,
		PanelListen:       panelAccess.PanelListen,
		PanelAccess:       input.PanelAccess,
		PanelTLSEnabled:   panelAccess.PanelTLSEnabled,
		PanelTLSCertPEM:   panelAccess.PanelTLSCertPEM,
		PanelTLSKeyPEM:    panelAccess.PanelTLSKeyPEM,
		WebBasePath:       panelAccess.WebBasePath,
		InstallPanelCaddy: panelAccess.InstallPanelCaddy,
		Caddyfile:         panelAccess.Caddyfile,
		MasqueradeURL:     masqueradeURL,
		FallbackRoot:      fallbackRoot,
	}, nil
}

func (m RURecommendedProfileModule) normalizedInput() RURecommendedInput {
	input := m.input
	if input.PanelAccess == "" {
		input.PanelAccess = "local"
	}
	if input.Secret == nil {
		input.Secret = func(label string) string { return label }
	}
	return input
}
