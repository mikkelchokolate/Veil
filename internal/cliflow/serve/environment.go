package serve

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

type Environment struct{}

func NewEnvironment() Environment { return Environment{} }

func (Environment) Listen(flagValue string) (listen string, source string) {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen, "--listen"
	}
	if listen := strings.TrimSpace(os.Getenv("VEIL_LISTEN")); listen != "" {
		return listen, "VEIL_LISTEN"
	}
	return "127.0.0.1:2096", "default"
}

func (Environment) AuthToken(flagValue string) (token string, source string) {
	if token := strings.TrimSpace(flagValue); token != "" {
		return token, "--auth-token"
	}
	if token := strings.TrimSpace(os.Getenv("VEIL_API_TOKEN")); token != "" {
		return token, "VEIL_API_TOKEN"
	}
	return "", "disabled"
}

func (Environment) ValidateListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("listen address must include a host (e.g. 127.0.0.1:%s or localhost:%s)", port, port)
	}
	if err := validatePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	return nil
}

func (Environment) ValidateAuthBinding(listen string, tokenSource string) error {
	if tokenSource != "disabled" {
		return nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if err := validatePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("API auth token is required when listening on non-loopback address %s; set --auth-token or VEIL_API_TOKEN", listen)
}

func (Environment) StatePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--state"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path, "VEIL_STATE_PATH"
	}
	return "/var/lib/veil/state.json", "default"
}

func (Environment) ApplyRoot(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--apply-root"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_APPLY_ROOT")); path != "" {
		return path, "VEIL_APPLY_ROOT"
	}
	return "/etc/veil", "default"
}

func (Environment) KeyPath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--key-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); path != "" {
		return path, "VEIL_KEY_PATH"
	}
	return "/etc/veil/state.key", "default"
}

func (Environment) PanelAccess() string { return strings.TrimSpace(os.Getenv("VEIL_PANEL_ACCESS")) }
func (Environment) Domain() string      { return strings.TrimSpace(os.Getenv("VEIL_DOMAIN")) }
func (Environment) Email() string       { return strings.TrimSpace(os.Getenv("VEIL_EMAIL")) }

func (Environment) WebBasePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return CleanWebBasePath(path), "--web-base-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_WEB_BASE_PATH")); path != "" {
		return CleanWebBasePath(path), "VEIL_WEB_BASE_PATH"
	}
	return "/", "default"
}

func (e Environment) TLS(cert, key string) (enabled bool, source string) {
	enabled, source, _, _ = e.TLSFiles(cert, key)
	return enabled, source
}

func (Environment) TLSFiles(cert, key string) (enabled bool, source string, certPath string, keyPath string) {
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

type AutoTLSConfig struct {
	Enabled  bool
	Domain   string
	Email    string
	CacheDir string
}

func (e Environment) AutoTLS(autoTLS bool, autoTLSDir string, statePath string, keyPath string) (AutoTLSConfig, error) {
	if !autoTLS && strings.TrimSpace(os.Getenv("VEIL_AUTO_TLS")) == "" {
		return AutoTLSConfig{}, nil
	}
	domain, email, err := e.SettingsFromState(statePath, keyPath)
	if err != nil {
		return AutoTLSConfig{}, fmt.Errorf("failed to read state for auto-tls: %w", err)
	}
	if domain == "" {
		return AutoTLSConfig{}, fmt.Errorf("auto-tls requires domain in state settings; set domain via API or veil install")
	}
	if email == "" {
		return AutoTLSConfig{}, fmt.Errorf("auto-tls requires email in state settings; set email via API or veil install")
	}
	if autoTLSDir == "" {
		autoTLSDir = strings.TrimSpace(os.Getenv("VEIL_AUTO_TLS_DIR"))
	}
	if autoTLSDir == "" {
		autoTLSDir = "/var/lib/veil/autocert"
	}
	return AutoTLSConfig{Enabled: true, Domain: domain, Email: email, CacheDir: autoTLSDir}, nil
}

func (Environment) SettingsFromState(statePath, keyPath string) (domain, email string, err error) {
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
	var snapshot model.ManagementSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return "", "", fmt.Errorf("parse state JSON: %w", err)
	}
	return snapshot.Settings.Domain, snapshot.Settings.Email, nil
}

func validatePort(port string) error {
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("must be 1-65535")
	}
	return nil
}

func CleanWebBasePath(path string) string {
	path = "/" + strings.Trim(path, "/")
	if path == "" {
		path = "/"
	}
	path += "/"
	return path
}
