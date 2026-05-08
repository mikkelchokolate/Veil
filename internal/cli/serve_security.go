package cli

import "fmt"

type ServeSecurity struct {
	opts serveWorkflowOptions
}

func NewServeSecurity(opts serveWorkflowOptions) ServeSecurity {
	return ServeSecurity{opts: opts}
}

func (s ServeSecurity) Resolve() (serveConfig, error) {
	opts := s.opts
	env := NewServeEnvironment()
	listen, listenSource := resolveServeListen(opts.Listen)
	if err := validateServeListen(listen); err != nil {
		return serveConfig{}, err
	}
	token, tokenSource := resolveServeAuthToken(opts.AuthToken)
	if err := validateServeAuthBinding(listen, tokenSource); err != nil {
		return serveConfig{}, err
	}
	statePath, stateSource := resolveServeStatePath(opts.StatePath)
	applyRoot, applyRootSource := resolveServeApplyRoot(opts.ApplyRoot)
	keyPath, keySource := resolveServeKeyPath(opts.KeyPath)
	webBasePath, _ := resolveServeWebBasePath(opts.WebBasePath)
	tlsEnabled, tlsSource, tlsCert, tlsKey := resolveServeTLSFiles(opts.TLSCert, opts.TLSKey)
	if opts.AutoTLS && !tlsEnabled {
		autoTLSEnabled, autoTLSErr := resolveServeAutoTLS(opts.AutoTLS, opts.AutoTLSDir, statePath, keyPath)
		if autoTLSErr != nil {
			return serveConfig{}, fmt.Errorf("auto-tls: %w", autoTLSErr)
		}
		tlsEnabled = autoTLSEnabled
		tlsSource = "auto-tls (Let's Encrypt)"
		tlsCert = ""
		tlsKey = ""
	}
	return serveConfig{
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
	}, nil
}
