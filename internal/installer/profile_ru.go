package installer

import (
	"fmt"

	"github.com/veil-panel/veil/internal/renderer"
)

type SecretFunc func(label string) string

type Stack string

const (
	StackPanel     Stack = "panel"
	StackMieru     Stack = "mieru"
	StackBoth      Stack = "both"
	StackNaive     Stack = "naive"
	StackHysteria2 Stack = "hysteria2"
)

type RURecommendedInput struct {
	Domain      string
	Email       string
	PanelAccess string
	Secret      SecretFunc
	PanelPort   int
}

type RURecommendedProfile struct {
	Domain             string
	Email              string
	Username           string
	NaivePassword      string
	Hysteria2Password  string
	PanelAuthToken     string
	PanelListen        string
	PanelAccess        string
	PanelTLSEnabled    bool
	PanelTLSCertPEM    string
	PanelTLSKeyPEM     string
	WebBasePath        string
	Stack              Stack
	InstallNaive       bool
	InstallHysteria2   bool
	InstallMieru       bool
	InstallPanelCaddy  bool
	PortPlan           SharedPortPlan
	Caddyfile          string
	Hysteria2YAML      string
	NaiveClientURL     string
	Hysteria2ClientURI string
	MasqueradeURL      string
	FallbackRoot       string
}

type RURecommendedProfileModule struct {
	input RURecommendedInput
}

type ruRecommendedStackPolicy = RURecommendedStackPolicy
type ruRecommendedNaiveArtifacts struct {
	Password  string
	Caddyfile string
	ClientURL string
}

type ruRecommendedHysteriaArtifacts struct {
	Password   string
	ServerYAML string
	ClientURI  string
}

func NewRURecommendedProfileModule(input RURecommendedInput) RURecommendedProfileModule {
	return RURecommendedProfileModule{input: input}
}

func BuildRURecommendedProfile(input RURecommendedInput) (RURecommendedProfile, error) {
	return NewRURecommendedProfileModule(input).Build()
}

func (m RURecommendedProfileModule) Build() (RURecommendedProfile, error) {
	input := m.normalizedInput()
	panelCaddy := input.PanelAccess == "caddy"
	if panelCaddy {
		if err := ValidateDomain(input.Domain); err != nil {
			return RURecommendedProfile{}, err
		}
		if err := ValidateEmail(input.Email); err != nil {
			return RURecommendedProfile{}, err
		}
	}
	plan := SharedPortPlan{}

	defaults := NewRURecommendedDefaults()
	username := defaults.Username
	masqueradeURL := defaults.MasqueradeURL
	fallbackRoot := defaults.FallbackRoot
	webBasePath := ""
	if panelCaddy {
		webBasePath = generateWebBasePath()
	}
	panelAuthToken := input.Secret("panel")
	panelListen := recommendedPanelListen(input.PanelAccess, input.PanelPort)
	panelTLS := PanelTLSMaterial{}
	var err error
	panelTLSEnabled := !panelCaddy
	if panelTLSEnabled {
		panelTLS, err = NewPanelTLS().Generate(input.Domain)
		if err != nil {
			return RURecommendedProfile{}, err
		}
	}
	caddyfile := ""
	if panelCaddy {
		caddyfile, err = renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort, WebBasePath: webBasePath})
		if err != nil {
			return RURecommendedProfile{}, err
		}
	}

	return RURecommendedProfile{
		Domain:             input.Domain,
		Email:              input.Email,
		Username:           username,
		NaivePassword:      "",
		Hysteria2Password:  "",
		PanelAuthToken:     panelAuthToken,
		PanelListen:        panelListen,
		PanelAccess:        input.PanelAccess,
		PanelTLSEnabled:    panelTLSEnabled,
		PanelTLSCertPEM:    panelTLS.CertPEM,
		PanelTLSKeyPEM:     panelTLS.KeyPEM,
		WebBasePath:        webBasePath,
		Stack:              StackPanel,
		InstallNaive:       false,
		InstallHysteria2:   false,
		InstallMieru:       false,
		InstallPanelCaddy:  panelCaddy,
		PortPlan:           plan,
		Caddyfile:          caddyfile,
		Hysteria2YAML:      "",
		NaiveClientURL:     "",
		Hysteria2ClientURI: "",
		MasqueradeURL:      masqueradeURL,
		FallbackRoot:       fallbackRoot,
	}, nil
}

func (m RURecommendedProfileModule) normalizedInput() RURecommendedInput {
	return NewRURecommendedInputDefaults().Apply(m.input)
}

func recommendedPanelListen(panelAccess string, panelPort int) string {
	if panelPort <= 0 {
		panelPort = 2096
	}
	if panelAccess == "direct" {
		return fmt.Sprintf("0.0.0.0:%d", panelPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", panelPort)
}
