package installer

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

type RURecommendedProfileModule struct {
	input RURecommendedInput
}

func NewRURecommendedProfileModule(input RURecommendedInput) RURecommendedProfileModule {
	return RURecommendedProfileModule{input: input}
}

func BuildRURecommendedProfile(input RURecommendedInput) (RURecommendedProfile, error) {
	return NewRURecommendedProfileModule(input).Build()
}

func (m RURecommendedProfileModule) Build() (RURecommendedProfile, error) {
	input := m.normalizedInput()
	defaults := NewRURecommendedDefaults()
	username := defaults.Username
	masqueradeURL := defaults.MasqueradeURL
	fallbackRoot := defaults.FallbackRoot
	panelAccess, err := NewPanelAccessProfile(PanelAccessProfileInput{PanelAccess: input.PanelAccess, Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort}).Build()
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
	return NewRURecommendedInputDefaults().Apply(m.input)
}
