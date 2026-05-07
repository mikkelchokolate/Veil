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
	if err := validateServeListen(opts.Listen); err != nil {
		return serveConfig{}, err
	}
	token, tokenSource := resolveServeAuthToken(opts.AuthToken)
	if err := validateServeAuthBinding(opts.Listen, tokenSource); err != nil {
		return serveConfig{}, err
	}
	statePath, stateSource := resolveServeStatePath(opts.StatePath)
	applyRoot, applyRootSource := resolveServeApplyRoot(opts.ApplyRoot)
	keyPath, keySource := resolveServeKeyPath(opts.KeyPath)
	webBasePath, _ := resolveServeWebBasePath(opts.WebBasePath)
	tlsEnabled, tlsSource := resolveServeTLS(opts.TLSCert, opts.TLSKey)
	if opts.AutoTLS && !tlsEnabled {
		autoTLSEnabled, autoTLSErr := resolveServeAutoTLS(opts.AutoTLS, opts.AutoTLSDir, statePath, keyPath)
		if autoTLSErr != nil {
			return serveConfig{}, fmt.Errorf("auto-tls: %w", autoTLSErr)
		}
		tlsEnabled = autoTLSEnabled
		tlsSource = "auto-tls (Let's Encrypt)"
	}
	return serveConfig{
		Token:           token,
		TokenSource:     tokenSource,
		StatePath:       statePath,
		StateSource:     stateSource,
		ApplyRoot:       applyRoot,
		ApplyRootSource: applyRootSource,
		KeyPath:         keyPath,
		KeySource:       keySource,
		WebBasePath:     webBasePath,
		TLSEnabled:      tlsEnabled,
		TLSSource:       tlsSource,
	}, nil
}
