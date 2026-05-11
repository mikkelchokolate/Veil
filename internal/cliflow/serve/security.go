package serve

import "fmt"

type SecurityOptions struct {
	Listen      string
	AuthToken   string
	StatePath   string
	ApplyRoot   string
	KeyPath     string
	TLSCert     string
	TLSKey      string
	WebBasePath string
	AutoTLS     bool
	AutoTLSDir  string
}

type Config struct {
	Listen          string
	ListenSource    string
	Token           string
	TokenSource     string
	StatePath       string
	StateSource     string
	ApplyRoot       string
	ApplyRootSource string
	KeyPath         string
	KeySource       string
	PanelAccess     string
	Domain          string
	Email           string
	WebBasePath     string
	TLSEnabled      bool
	TLSSource       string
	TLSCert         string
	TLSKey          string
	AutoTLSDomain   string
	AutoTLSEmail    string
	AutoTLSCacheDir string
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
	token, tokenSource := env.AuthToken(opts.AuthToken)
	if err := env.ValidateAuthBinding(listen, tokenSource); err != nil {
		return Config{}, err
	}
	statePath, stateSource := env.StatePath(opts.StatePath)
	applyRoot, applyRootSource := env.ApplyRoot(opts.ApplyRoot)
	keyPath, keySource := env.KeyPath(opts.KeyPath)
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
	return Config{
		Listen:          listen,
		ListenSource:    listenSource,
		Token:           token,
		TokenSource:     tokenSource,
		StatePath:       statePath,
		StateSource:     stateSource,
		ApplyRoot:       applyRoot,
		ApplyRootSource: applyRootSource,
		KeyPath:         keyPath,
		KeySource:       keySource,
		PanelAccess:     env.PanelAccess(),
		Domain:          env.Domain(),
		Email:           env.Email(),
		WebBasePath:     webBasePath,
		TLSEnabled:      tlsEnabled,
		TLSSource:       tlsSource,
		TLSCert:         tlsCert,
		TLSKey:          tlsKey,
		AutoTLSDomain:   autoTLSConfig.Domain,
		AutoTLSEmail:    autoTLSConfig.Email,
		AutoTLSCacheDir: autoTLSConfig.CacheDir,
	}, nil
}
