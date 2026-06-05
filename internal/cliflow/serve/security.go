package serve

import "fmt"

type SecurityOptions struct {
	Listen                string
	AuthToken             string
	MetricsAccess         string
	StatePath             string
	ApplyRoot             string
	KeyPath               string
	TLSCert               string
	TLSKey                string
	WebBasePath           string
	AutoTLS               bool
	AutoTLSDir            string
	AllowUnsafePublicHTTP bool
}

type Config struct {
	Listen                string
	ListenSource          string
	Token                 string
	TokenSource           string
	PublicListen          bool
	SessionAuthConfigured bool
	MetricsAccess         string
	MetricsAccessSource   string
	MetricsAuthRequired   bool
	StatePath             string
	StateSource           string
	ApplyRoot             string
	ApplyRootSource       string
	KeyPath               string
	KeySource             string
	PanelAccess           string
	Domain                string
	Email                 string
	WebBasePath           string
	TLSEnabled            bool
	TLSSource             string
	TLSCert               string
	TLSKey                string
	AutoTLSDomain         string
	AutoTLSEmail          string
	AutoTLSCacheDir       string
	AllowUnsafePublicHTTP bool
}

type Security struct {
	opts SecurityOptions
}

func NewSecurity(opts SecurityOptions) Security {
	return Security{opts: opts}
}

func (s Security) Resolve() (Config, error) {
	opts := s.opts
	env := NewEnvironment()
	listen, listenSource := env.Listen(opts.Listen)
	if err := env.ValidateListen(listen); err != nil {
		return Config{}, err
	}
	publicListen, err := env.IsPublicListen(listen)
	if err != nil {
		return Config{}, err
	}
	token, tokenSource := env.AuthToken(opts.AuthToken)
	statePath, stateSource := env.StatePath(opts.StatePath)
	applyRoot, applyRootSource := env.ApplyRoot(opts.ApplyRoot)
	keyPath, keySource := env.KeyPath(opts.KeyPath)
	panelAccess := env.PanelAccess()
	exposed := publicListen || panelAccess == "direct" || panelAccess == "caddy"
	sessionAuthConfigured := false
	if exposed {
		var sessionErr error
		sessionAuthConfigured, sessionErr = env.SessionAuthConfigured(statePath)
		if sessionErr != nil {
			return Config{}, fmt.Errorf("session auth check: %w", sessionErr)
		}
	}
	metricsAccess, metricsAccessSource := env.MetricsAccess(opts.MetricsAccess)
	metricsAuthRequired, err := env.MetricsAuthRequired(metricsAccess, exposed, tokenSource, sessionAuthConfigured)
	if err != nil {
		return Config{}, err
	}
	webBasePath, _ := env.WebBasePath(opts.WebBasePath)
	tlsEnabled, tlsSource, tlsCert, tlsKey := env.TLSFiles(opts.TLSCert, opts.TLSKey)
	autoTLSConfig := AutoTLSConfig{}
	if opts.AutoTLS && !tlsEnabled {
		var autoTLSErr error
		autoTLSConfig, autoTLSErr = env.AutoTLS(opts.AutoTLS, opts.AutoTLSDir, statePath, keyPath)
		if autoTLSErr != nil {
			return Config{}, fmt.Errorf("auto-tls: %w", autoTLSErr)
		}
		tlsEnabled = autoTLSConfig.Enabled
		tlsSource = "auto-tls (Let's Encrypt)"
		tlsCert = ""
		tlsKey = ""
	}
	allowUnsafePublicHTTP, err := env.AllowUnsafePublicHTTP(opts.AllowUnsafePublicHTTP)
	if err != nil {
		return Config{}, err
	}
	if err := NewExposurePolicy().Validate(ExposureInput{
		PanelAccess:           panelAccess,
		PublicListen:          publicListen,
		TokenConfigured:       tokenSource != "disabled",
		SessionAuthConfigured: sessionAuthConfigured,
		MetricsAuthRequired:   metricsAuthRequired,
		NativeTLS:             tlsEnabled,
		ProxyTLS:              panelAccess == "caddy",
		AllowUnsafePublicHTTP: allowUnsafePublicHTTP,
	}); err != nil {
		return Config{}, err
	}
	return Config{
		Listen:                listen,
		ListenSource:          listenSource,
		Token:                 token,
		TokenSource:           tokenSource,
		PublicListen:          publicListen,
		SessionAuthConfigured: sessionAuthConfigured,
		MetricsAccess:         metricsAccess,
		MetricsAccessSource:   metricsAccessSource,
		MetricsAuthRequired:   metricsAuthRequired,
		StatePath:             statePath,
		StateSource:           stateSource,
		ApplyRoot:             applyRoot,
		ApplyRootSource:       applyRootSource,
		KeyPath:               keyPath,
		KeySource:             keySource,
		PanelAccess:           panelAccess,
		Domain:                env.Domain(),
		Email:                 env.Email(),
		WebBasePath:           webBasePath,
		TLSEnabled:            tlsEnabled,
		TLSSource:             tlsSource,
		TLSCert:               tlsCert,
		TLSKey:                tlsKey,
		AutoTLSDomain:         autoTLSConfig.Domain,
		AutoTLSEmail:          autoTLSConfig.Email,
		AutoTLSCacheDir:       autoTLSConfig.CacheDir,
		AllowUnsafePublicHTTP: allowUnsafePublicHTTP,
	}, nil
}
