package cli

import serveflow "github.com/veil-panel/veil/internal/cliflow/serve"

type ServeEnvironment struct{}

func NewServeEnvironment() ServeEnvironment { return ServeEnvironment{} }

func (ServeEnvironment) Listen(flagValue string) (listen string, source string) {
	return serveflow.NewEnvironment().Listen(flagValue)
}

func (ServeEnvironment) AuthToken(flagValue string) (token string, source string) {
	return serveflow.NewEnvironment().AuthToken(flagValue)
}

func (ServeEnvironment) ValidateListen(listen string) error {
	return serveflow.NewEnvironment().ValidateListen(listen)
}

func (ServeEnvironment) ValidateAuthBinding(listen string, tokenSource string) error {
	return serveflow.NewEnvironment().ValidateAuthBinding(listen, tokenSource)
}

func (ServeEnvironment) StatePath(flagValue string) (path string, source string) {
	return serveflow.NewEnvironment().StatePath(flagValue)
}

func (ServeEnvironment) ApplyRoot(flagValue string) (path string, source string) {
	return serveflow.NewEnvironment().ApplyRoot(flagValue)
}

func (ServeEnvironment) KeyPath(flagValue string) (path string, source string) {
	return serveflow.NewEnvironment().KeyPath(flagValue)
}

func (ServeEnvironment) PanelAccess() string { return serveflow.NewEnvironment().PanelAccess() }
func (ServeEnvironment) Domain() string      { return serveflow.NewEnvironment().Domain() }
func (ServeEnvironment) Email() string       { return serveflow.NewEnvironment().Email() }

func (ServeEnvironment) WebBasePath(flagValue string) (path string, source string) {
	return serveflow.NewEnvironment().WebBasePath(flagValue)
}

func (e ServeEnvironment) TLS(cert, key string) (enabled bool, source string) {
	return serveflow.NewEnvironment().TLS(cert, key)
}

func (ServeEnvironment) TLSFiles(cert, key string) (enabled bool, source string, certPath string, keyPath string) {
	return serveflow.NewEnvironment().TLSFiles(cert, key)
}

func (e ServeEnvironment) AutoTLS(autoTLS bool, autoTLSDir string, statePath string, keyPath string) (enabled bool, err error) {
	cfg, err := serveflow.NewEnvironment().AutoTLS(autoTLS, autoTLSDir, statePath, keyPath)
	if err != nil || !cfg.Enabled {
		return false, err
	}
	autoTLSDomain = cfg.Domain
	autoTLSEmail = cfg.Email
	autoTLSCacheDir = cfg.CacheDir
	return true, nil
}

func (ServeEnvironment) SettingsFromState(statePath, keyPath string) (domain, email string, err error) {
	return serveflow.NewEnvironment().SettingsFromState(statePath, keyPath)
}

func resolveServeListen(flagValue string) (string, string) {
	return NewServeEnvironment().Listen(flagValue)
}
func resolveServeAuthToken(flagValue string) (string, string) {
	return NewServeEnvironment().AuthToken(flagValue)
}
func validateServeListen(listen string) error { return NewServeEnvironment().ValidateListen(listen) }
func validateServeAuthBinding(listen string, tokenSource string) error {
	return NewServeEnvironment().ValidateAuthBinding(listen, tokenSource)
}
func resolveServeStatePath(flagValue string) (string, string) {
	return NewServeEnvironment().StatePath(flagValue)
}
func resolveServeApplyRoot(flagValue string) (string, string) {
	return NewServeEnvironment().ApplyRoot(flagValue)
}
func resolveServeKeyPath(flagValue string) (string, string) {
	return NewServeEnvironment().KeyPath(flagValue)
}
func resolveServeWebBasePath(flagValue string) (string, string) {
	return NewServeEnvironment().WebBasePath(flagValue)
}
func resolveServeAutoTLS(autoTLS bool, autoTLSDir string, statePath string, keyPath string) (bool, error) {
	return NewServeEnvironment().AutoTLS(autoTLS, autoTLSDir, statePath, keyPath)
}
func readSettingsFromState(statePath, keyPath string) (string, string, error) {
	return NewServeEnvironment().SettingsFromState(statePath, keyPath)
}
func resolveServeTLS(cert, key string) (bool, string) { return NewServeEnvironment().TLS(cert, key) }
func resolveServeTLSFiles(cert, key string) (bool, string, string, string) {
	return NewServeEnvironment().TLSFiles(cert, key)
}
