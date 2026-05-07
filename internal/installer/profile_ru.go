package installer

type SecretFunc func(label string) string

type Stack string

const (
	StackBoth      Stack = "both"
	StackNaive     Stack = "naive"
	StackHysteria2 Stack = "hysteria2"
)

type RURecommendedInput struct {
	Domain       string
	Email        string
	Stack        Stack
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
	WebBasePath        string
	Stack              Stack
	InstallNaive       bool
	InstallHysteria2   bool
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
	if err := ValidateDomain(input.Domain); err != nil {
		return RURecommendedProfile{}, err
	}
	if err := ValidateEmail(input.Email); err != nil {
		return RURecommendedProfile{}, err
	}
	stack, err := m.stackPolicy(input.Stack)
	if err != nil {
		return RURecommendedProfile{}, err
	}
	plan, err := m.portPlan(input, stack)
	if err != nil {
		return RURecommendedProfile{}, err
	}

	defaults := NewRURecommendedDefaults()
	username := defaults.Username
	masqueradeURL := defaults.MasqueradeURL
	fallbackRoot := defaults.FallbackRoot
	webBasePath := generateWebBasePath()
	panelAuthToken := input.Secret("panel")
	naive := ruRecommendedNaiveArtifacts{}
	hysteria := ruRecommendedHysteriaArtifacts{}

	if stack.InstallNaive {
		naive, err = m.naiveArtifacts(input, plan, username, fallbackRoot, webBasePath)
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
		WebBasePath:        webBasePath,
		Stack:              stack.Stack,
		InstallNaive:       stack.InstallNaive,
		InstallHysteria2:   stack.InstallHysteria2,
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
	normalized, installNaive, installHysteria2, err := normalizeStack(stack)
	if err != nil {
		return ruRecommendedStackPolicy{}, err
	}
	return RURecommendedStackPolicy{Stack: normalized, InstallNaive: installNaive, InstallHysteria2: installHysteria2}, nil
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
