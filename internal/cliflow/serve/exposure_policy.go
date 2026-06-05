package serve

import "fmt"

type ExposureInput struct {
	PanelAccess           string
	PublicListen          bool
	TokenConfigured       bool
	SessionAuthConfigured bool
	MetricsAuthRequired   bool
	NativeTLS             bool
	ProxyTLS              bool
	AllowUnsafePublicHTTP bool
}

type ExposurePolicy struct{}

func NewExposurePolicy() ExposurePolicy { return ExposurePolicy{} }

func (ExposurePolicy) Validate(in ExposureInput) error {
	exposed := in.PublicListen || in.PanelAccess == "direct" || in.PanelAccess == "caddy"
	if !exposed {
		return nil
	}
	if !in.SessionAuthConfigured {
		return fmt.Errorf("public Panel listen or reverse-proxy exposure requires user/session auth; run `veil admin reset` or `veil admin set --username admin --password <password>`")
	}
	if (in.PublicListen || in.PanelAccess == "direct") && !in.TokenConfigured {
		return fmt.Errorf("direct public Panel exposure requires an API token")
	}
	if !in.MetricsAuthRequired {
		return fmt.Errorf("public Panel exposure requires authenticated metrics")
	}
	if !in.NativeTLS && !in.ProxyTLS && !in.AllowUnsafePublicHTTP {
		return fmt.Errorf("public Panel exposure requires TLS; unsafe HTTP requires an explicit override")
	}
	return nil
}
