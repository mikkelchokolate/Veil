package serve

import (
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

type SecurityOptions struct {
	Listen                string
	AuthToken             string
	MetricsAccess         string
	StatePath             string
	ApplyRoot             string
	LiveRoot              string
	KeyPath               string
	HelperSocket          string
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
	// MetricsAuthWhenUsersConfigured is set for --metrics-access auto: the
	// router re-evaluates the "authenticate whenever auth is configured"
	// term against the live user set, so a Panel user created by first-run
	// setup after startup also protects /metrics (#1433).
	MetricsAuthWhenUsersConfigured bool
	StatePath                      string
	StateSource                    string
	ApplyRoot                      string
	ApplyRootSource                string
	LiveRoot                       string
	LiveRootSource                 string
	KeyPath                        string
	KeySource                      string
	HelperSocket                   string
	HelperSocketSource             string
	PanelAccess                    string
	Domain                         string
	Email                          string
	WebBasePath                    string
	TLSEnabled                     bool
	TLSSource                      string
	TLSCert                        string
	TLSKey                         string
	AutoTLSDomain                  string
	AutoTLSEmail                   string
	AutoTLSCacheDir                string
	AllowUnsafePublicHTTP          bool
	SetupAllowed                   bool
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
	liveRoot, liveRootSource := env.LiveRoot(opts.LiveRoot)
	keyPath, keySource := env.KeyPath(opts.KeyPath)
	helperSocket, helperSocketSource := env.HelperSocket(opts.HelperSocket)
	panelAccess := env.PanelAccess()
	exposed := publicListen || panelAccess == "direct" || panelAccess == "caddy"
	// Probe session auth for every Panel, not only exposed ones: the auto
	// metrics rule and the startup banner both depend on it (#1433). An
	// unreadable state file stays fatal only for exposed Panels, where the
	// exposure policy needs a definite answer; a loopback Panel keeps
	// starting and the router reports the state load failure itself.
	sessionAuthConfigured, sessionErr := env.SessionAuthConfigured(statePath)
	if sessionErr != nil {
		if exposed {
			return Config{}, fmt.Errorf("session auth check: %w", sessionErr)
		}
		sessionAuthConfigured = false
	}
	metricsAccess, metricsAccessSource := env.MetricsAccess(opts.MetricsAccess)
	metricsAuthRequired, err := env.MetricsAuthRequired(metricsAccess, exposed, tokenSource, sessionAuthConfigured)
	if err != nil {
		return Config{}, err
	}
	webBasePath, _ := env.WebBasePath(opts.WebBasePath)
	webBasePath, err = webbasepath.Normalize(webBasePath)
	if err != nil {
		return Config{}, fmt.Errorf("web base path: %w", err)
	}
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
	setupAllowed := !exposed && (panelAccess == "" || panelAccess == "local")
	return Config{
		Listen:                         listen,
		ListenSource:                   listenSource,
		Token:                          token,
		TokenSource:                    tokenSource,
		PublicListen:                   publicListen,
		SessionAuthConfigured:          sessionAuthConfigured,
		MetricsAccess:                  metricsAccess,
		MetricsAccessSource:            metricsAccessSource,
		MetricsAuthRequired:            metricsAuthRequired,
		MetricsAuthWhenUsersConfigured: metricsAccess == "auto",
		StatePath:                      statePath,
		StateSource:                    stateSource,
		ApplyRoot:                      applyRoot,
		ApplyRootSource:                applyRootSource,
		LiveRoot:                       liveRoot,
		LiveRootSource:                 liveRootSource,
		KeyPath:                        keyPath,
		KeySource:                      keySource,
		HelperSocket:                   helperSocket,
		HelperSocketSource:             helperSocketSource,
		PanelAccess:                    panelAccess,
		Domain:                         env.Domain(),
		Email:                          env.Email(),
		WebBasePath:                    webBasePath,
		TLSEnabled:                     tlsEnabled,
		TLSSource:                      tlsSource,
		TLSCert:                        tlsCert,
		TLSKey:                         tlsKey,
		AutoTLSDomain:                  autoTLSConfig.Domain,
		AutoTLSEmail:                   autoTLSConfig.Email,
		AutoTLSCacheDir:                autoTLSConfig.CacheDir,
		AllowUnsafePublicHTTP:          allowUnsafePublicHTTP,
		SetupAllowed:                   setupAllowed,
	}, nil
}
