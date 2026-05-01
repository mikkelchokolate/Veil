package installer

import (
	"fmt"
	"net/url"

	"github.com/veil-panel/veil/internal/renderer"
)

type SecretFunc func(label string) string

type Stack string

const (
	StackBoth      Stack = "both"
	StackNaive     Stack = "naive"
	StackHysteria2 Stack = "hysteria2"
)

func normalizeStack(stack Stack) (normalized Stack, installNaive bool, installHysteria2 bool, err error) {
	switch stack {
	case "", StackBoth:
		return StackBoth, true, true, nil
	case StackNaive:
		return StackNaive, true, false, nil
	case StackHysteria2:
		return StackHysteria2, false, true, nil
	default:
		return "", false, false, fmt.Errorf("unsupported stack %q", stack)
	}
}

type RURecommendedInput struct {
	Domain       string
	Email        string
	Stack        Stack
	Availability PortAvailability
	Secret       SecretFunc
	RandomPort   func() int
}

type RURecommendedProfile struct {
	Domain             string
	Email              string
	Username           string
	NaivePassword      string
	Hysteria2Password  string
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

func BuildRURecommendedProfile(input RURecommendedInput) (RURecommendedProfile, error) {
	if err := ValidateDomain(input.Domain); err != nil {
		return RURecommendedProfile{}, err
	}
	if err := ValidateEmail(input.Email); err != nil {
		return RURecommendedProfile{}, err
	}
	if input.Secret == nil {
		input.Secret = func(label string) string { return label }
	}
	if input.RandomPort == nil {
		input.RandomPort = func() int { return 443 }
	}
	stack, installNaive, installHysteria2, err := normalizeStack(input.Stack)
	if err != nil {
		return RURecommendedProfile{}, err
	}

	plan := PlanStackPort(input.Availability, []int{443, 8443}, input.RandomPort, installNaive, installHysteria2)
	username := "veil"
	masqueradeURL := "https://www.bing.com/"
	fallbackRoot := "/var/lib/veil/www"
	var naivePassword string
	var hysteriaPassword string
	var caddyfile string
	var hysteriaYAML string
	var naiveClientURL string
	var hysteriaClientURI string

	if installNaive {
		naivePassword = input.Secret("naive")
		caddyfile, err = renderer.RenderNaiveCaddyfile(renderer.NaiveConfig{
			Domain:       input.Domain,
			Email:        input.Email,
			ListenPort:   plan.Port,
			Username:     username,
			Password:     naivePassword,
			FallbackRoot: fallbackRoot,
		})
		if err != nil {
			return RURecommendedProfile{}, err
		}
		naiveClientURL = naiveURL(username, naivePassword, input.Domain, plan.Port)
	}
	if installHysteria2 {
		hysteriaPassword = input.Secret("hysteria2")
		hysteriaYAML, err = renderer.RenderHysteria2(renderer.Hysteria2Config{
			ListenPort:    plan.Port,
			Domain:        input.Domain,
			Password:      hysteriaPassword,
			MasqueradeURL: masqueradeURL,
		})
		if err != nil {
			return RURecommendedProfile{}, err
		}
		hysteriaClientURI = hysteria2URI(hysteriaPassword, input.Domain, plan.Port)
	}

	return RURecommendedProfile{
		Domain:             input.Domain,
		Email:              input.Email,
		Username:           username,
		NaivePassword:      naivePassword,
		Hysteria2Password:  hysteriaPassword,
		Stack:              stack,
		InstallNaive:       installNaive,
		InstallHysteria2:   installHysteria2,
		PortPlan:           plan,
		Caddyfile:          caddyfile,
		Hysteria2YAML:      hysteriaYAML,
		NaiveClientURL:     naiveClientURL,
		Hysteria2ClientURI: hysteriaClientURI,
		MasqueradeURL:      masqueradeURL,
		FallbackRoot:       fallbackRoot,
	}, nil
}

func naiveURL(username, password, domain string, port int) string {
	u := url.URL{
		Scheme: "https",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", domain, port),
	}
	return u.String()
}

func hysteria2URI(password, domain string, port int) string {
	u := url.URL{
		Scheme: "hysteria2",
		User:   url.User(password),
		Host:   fmt.Sprintf("%s:%d", domain, port),
	}
	q := u.Query()
	q.Set("insecure", "0")
	u.RawQuery = q.Encode()
	return u.String()
}
