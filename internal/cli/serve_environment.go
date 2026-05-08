package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/veil-panel/veil/internal/secrets"
)

type ServeEnvironment struct{}

func NewServeEnvironment() ServeEnvironment { return ServeEnvironment{} }

func (ServeEnvironment) Listen(flagValue string) (listen string, source string) {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen, "--listen"
	}
	if listen := strings.TrimSpace(os.Getenv("VEIL_LISTEN")); listen != "" {
		return listen, "VEIL_LISTEN"
	}
	return "127.0.0.1:2096", "default"
}

func (ServeEnvironment) AuthToken(flagValue string) (token string, source string) {
	if token := strings.TrimSpace(flagValue); token != "" {
		return token, "--auth-token"
	}
	if token := strings.TrimSpace(os.Getenv("VEIL_API_TOKEN")); token != "" {
		return token, "VEIL_API_TOKEN"
	}
	return "", "disabled"
}

func (ServeEnvironment) ValidateListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("listen address must include a host (e.g. 127.0.0.1:%s or localhost:%s)", port, port)
	}
	if err := validateServePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	return nil
}

func (ServeEnvironment) ValidateAuthBinding(listen string, tokenSource string) error {
	if tokenSource != "disabled" {
		return nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if err := validateServePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("API auth token is required when listening on non-loopback address %s; set --auth-token or VEIL_API_TOKEN", listen)
}

func (ServeEnvironment) StatePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--state"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path, "VEIL_STATE_PATH"
	}
	return "/var/lib/veil/state.json", "default"
}

func (ServeEnvironment) ApplyRoot(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--apply-root"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_APPLY_ROOT")); path != "" {
		return path, "VEIL_APPLY_ROOT"
	}
	return "/etc/veil", "default"
}

func (ServeEnvironment) KeyPath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--key-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); path != "" {
		return path, "VEIL_KEY_PATH"
	}
	return "/etc/veil/state.key", "default"
}

func (ServeEnvironment) PanelAccess() string {
	return strings.TrimSpace(os.Getenv("VEIL_PANEL_ACCESS"))
}

func (ServeEnvironment) Domain() string {
	return strings.TrimSpace(os.Getenv("VEIL_DOMAIN"))
}

func (ServeEnvironment) Email() string {
	return strings.TrimSpace(os.Getenv("VEIL_EMAIL"))
}

func (ServeEnvironment) WebBasePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return cleanWebBasePath(path), "--web-base-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_WEB_BASE_PATH")); path != "" {
		return cleanWebBasePath(path), "VEIL_WEB_BASE_PATH"
	}
	return "/", "default"
}

func (e ServeEnvironment) TLS(cert, key string) (enabled bool, source string) {
	enabled, source, _, _ = e.TLSFiles(cert, key)
	return enabled, source
}

func (ServeEnvironment) TLSFiles(cert, key string) (enabled bool, source string, certPath string, keyPath string) {
	if cert != "" && key != "" {
		return true, "--tls-cert / --tls-key", cert, key
	}
	if c := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")); c != "" {
		if k := strings.TrimSpace(os.Getenv("VEIL_TLS_KEY")); k != "" {
			return true, "VEIL_TLS_CERT / VEIL_TLS_KEY", c, k
		}
	}
	return false, "", "", ""
}

func (e ServeEnvironment) AutoTLS(autoTLS bool, autoTLSDir string, statePath string, keyPath string) (enabled bool, err error) {
	if !autoTLS && strings.TrimSpace(os.Getenv("VEIL_AUTO_TLS")) == "" {
		return false, nil
	}
	domain, email, err := e.SettingsFromState(statePath, keyPath)
	if err != nil {
		return false, fmt.Errorf("failed to read state for auto-tls: %w", err)
	}
	if domain == "" {
		return false, fmt.Errorf("auto-tls requires domain in state settings; set domain via API or veil install")
	}
	if email == "" {
		return false, fmt.Errorf("auto-tls requires email in state settings; set email via API or veil install")
	}
	if autoTLSDir == "" {
		autoTLSDir = strings.TrimSpace(os.Getenv("VEIL_AUTO_TLS_DIR"))
	}
	if autoTLSDir == "" {
		autoTLSDir = "/var/lib/veil/autocert"
	}
	autoTLSDomain = domain
	autoTLSEmail = email
	autoTLSCacheDir = autoTLSDir
	return true, nil
}

func (ServeEnvironment) SettingsFromState(statePath, keyPath string) (domain, email string, err error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return "", "", fmt.Errorf("read state file %s: %w", statePath, err)
	}
	if keyPath != "" {
		key, keyErr := secrets.LoadOrCreateKey(keyPath)
		if keyErr == nil {
			if ciph, ciphErr := secrets.NewCipher(*key); ciphErr == nil {
				if decrypted, decErr := ciph.Decrypt(string(data)); decErr == nil {
					data = []byte(decrypted)
				}
			}
		}
	}
	var snapshot stateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return "", "", fmt.Errorf("parse state JSON: %w", err)
	}
	return snapshot.Settings.Domain, snapshot.Settings.Email, nil
}

func validateServePort(port string) error {
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("must be 1-65535")
	}
	return nil
}

func cleanWebBasePath(path string) string {
	path = "/" + strings.Trim(path, "/")
	if path == "" {
		path = "/"
	}
	path += "/"
	return path
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
