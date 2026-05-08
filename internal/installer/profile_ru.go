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
	Domain       string
	Email        string
	Stack        Stack
	PanelAccess  string
	Port         int
	Availability PortAvailability
	Secret       SecretFunc
	RandomPort   func() int
	PanelPort    int
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
	stack, err := m.stackPolicy(input.Stack)
	if err != nil {
		return RURecommendedProfile{}, err
	}
	panelCaddy := input.PanelAccess == "caddy"
	if stack.RequiresDomain() || panelCaddy {
		if err := ValidateDomain(input.Domain); err != nil {
			return RURecommendedProfile{}, err
		}
		if err := ValidateEmail(input.Email); err != nil {
			return RURecommendedProfile{}, err
		}
	}
	plan, err := m.portPlan(input, stack)
	if err != nil {
		return RURecommendedProfile{}, err
	}

	defaults := NewRURecommendedDefaults()
	username := defaults.Username
	masqueradeURL := defaults.MasqueradeURL
	fallbackRoot := defaults.FallbackRoot
	webBasePath := ""
	if stack.InstallNaive || panelCaddy {
		webBasePath = generateWebBasePath()
	}
	panelAuthToken := input.Secret("panel")
	panelListen := recommendedPanelListen(input.PanelAccess, input.PanelPort)
	naive := ruRecommendedNaiveArtifacts{}
	hysteria := ruRecommendedHysteriaArtifacts{}

	if stack.InstallNaive {
		naive, err = m.naiveArtifacts(input, plan, username, fallbackRoot, webBasePath)
		if err != nil {
			return RURecommendedProfile{}, err
		}
	} else if panelCaddy {
		naive.Caddyfile, err = renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort, WebBasePath: webBasePath})
		if err != nil {
			return RURecommendedProfile{}, err
		}
	}
	if stack.InstallHysteria2 {
		hysteria, err = m.hysteriaArtifacts(input, plan, masqueradeURL)
		if err != nil {
			return RURecommendedProfile{}, err
		}
	}

	return RURecommendedProfile{
		Domain:             input.Domain,
		Email:              input.Email,
		Username:           username,
		NaivePassword:      naive.Password,
		Hysteria2Password:  hysteria.Password,
		PanelAuthToken:     panelAuthToken,
		PanelListen:        panelListen,
		PanelAccess:        input.PanelAccess,
		WebBasePath:        webBasePath,
		Stack:              stack.Stack,
		InstallNaive:       stack.InstallNaive,
		InstallHysteria2:   stack.InstallHysteria2,
		InstallMieru:       stack.InstallMieru,
		InstallPanelCaddy:  panelCaddy,
		PortPlan:           plan,
		Caddyfile:          naive.Caddyfile,
		Hysteria2YAML:      hysteria.ServerYAML,
		NaiveClientURL:     naive.ClientURL,
		Hysteria2ClientURI: hysteria.ClientURI,
		MasqueradeURL:      masqueradeURL,
		FallbackRoot:       fallbackRoot,
	}, nil
}

func (m RURecommendedProfileModule) normalizedInput() RURecommendedInput {
	return NewRURecommendedInputDefaults().Apply(m.input)
}

func (RURecommendedProfileModule) stackPolicy(stack Stack) (ruRecommendedStackPolicy, error) {
	return NewRURecommendedStackPolicy(stack)
}

func (RURecommendedProfileModule) portPlan(input RURecommendedInput, stack ruRecommendedStackPolicy) (SharedPortPlan, error) {
	return NewRURecommendedPortPolicy(input.Availability, input.RandomPort).Plan(input.Port, stack)
}

func (RURecommendedProfileModule) naiveArtifacts(input RURecommendedInput, plan SharedPortPlan, username, fallbackRoot, webBasePath string) (ruRecommendedNaiveArtifacts, error) {
	return NewRURecommendedNaiveArtifacts().Build(input, plan, RURecommendedDefaults{Username: username, FallbackRoot: fallbackRoot}, webBasePath)
}

func (RURecommendedProfileModule) hysteriaArtifacts(input RURecommendedInput, plan SharedPortPlan, masqueradeURL string) (ruRecommendedHysteriaArtifacts, error) {
	return NewRURecommendedHysteriaArtifacts().Build(input, plan, RURecommendedDefaults{MasqueradeURL: masqueradeURL})
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
