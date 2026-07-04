package serve

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/privileged"
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
	public, err := Environment{}.IsPublicListen(listen)
	if err != nil {
		return err
	}
	if !public || tokenSource != "disabled" {
		return nil
	}
	return fmt.Errorf("API auth token is required when listening on non-loopback address %s; set --auth-token or VEIL_API_TOKEN", listen)
}

func (Environment) ValidatePublicExposure(listen string, tokenSource string, sessionAuthConfigured bool) error {
	public, err := Environment{}.IsPublicListen(listen)
	if err != nil {
		return err
	}
	if !public {
		return nil
	}
	missingToken := tokenSource == "disabled"
	missingSession := !sessionAuthConfigured
	if !missingToken && !missingSession {
		return nil
	}
	return fmt.Errorf("public Panel listen %s requires both API token auth (--auth-token or VEIL_API_TOKEN) and user/session auth (run `veil admin reset` or `veil admin set --username admin --password <password>` before first public start)", listen)
}

func (Environment) IsPublicListen(listen string) (bool, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return false, fmt.Errorf("listen address must be host:port: %w", err)
	}
	if err := validatePort(port); err != nil {
		return false, fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	return !isLoopbackHost(host), nil
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}

func (Environment) SessionAuthConfigured(statePath string) (bool, error) {
	snapshot, ok, err := managementstate.NewStore(statePath, nil).Load()
	if err != nil {
		return false, fmt.Errorf("read management state users from %s: %w", statePath, err)
	}
	if !ok {
		return false, nil
	}
	return len(snapshot.Users) > 0, nil
}

func (Environment) MetricsAccess(flagValue string) (access string, source string) {
	if access := strings.ToLower(strings.TrimSpace(flagValue)); access != "" {
		return access, "--metrics-access"
	}
	if access := strings.ToLower(strings.TrimSpace(os.Getenv("VEIL_METRICS_ACCESS"))); access != "" {
		return access, "VEIL_METRICS_ACCESS"
	}
	return "auto", "default"
}

func (Environment) MetricsAuthRequired(access string, publicListen bool, tokenSource string, sessionAuthConfigured bool) (bool, error) {
	switch access {
	case "auto":
		return publicListen || tokenSource != "disabled" || sessionAuthConfigured, nil
	case "authenticated":
		return true, nil
	case "public":
		if publicListen {
			return false, fmt.Errorf("/metrics cannot be public on non-loopback Panel listen; use --metrics-access authenticated")
		}
		return false, nil
	default:
		return false, fmt.Errorf("--metrics-access must be one of auto, authenticated, or public")
	}
}

func (Environment) StatePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--state"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path, "VEIL_STATE_PATH"
	}
	if goos == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Veil", "state.json"), "default"
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
	if goos == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Veil"), "default"
	}
	return "/var/lib/veil/staging", "default"
}

func (Environment) LiveRoot(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--live-root"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_LIVE_ROOT")); path != "" {
		return path, "VEIL_LIVE_ROOT"
	}
	if goos == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Veil", "live"), "default"
	}
	return "/etc/veil/generated", "default"
}

func (Environment) KeyPath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--key-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); path != "" {
		return path, "VEIL_KEY_PATH"
	}
	if goos == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Veil", "state.key"), "default"
	}
	return "/etc/veil/state.key", "default"
}

func (Environment) HelperSocket(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--helper-socket"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_HELPER_SOCKET")); path != "" {
		return path, "VEIL_HELPER_SOCKET"
	}
	return privileged.DefaultSocketPath, "default"
}

func (Environment) PanelAccess() string { return strings.TrimSpace(os.Getenv("VEIL_PANEL_ACCESS")) }
func (Environment) Domain() string      { return strings.TrimSpace(os.Getenv("VEIL_DOMAIN")) }
func (Environment) Email() string       { return strings.TrimSpace(os.Getenv("VEIL_EMAIL")) }

func (Environment) AllowUnsafePublicHTTP(flagValue bool) (bool, error) {
	if flagValue {
		return true, nil
	}
	value := strings.TrimSpace(os.Getenv("VEIL_UNSAFE_ALLOW_PUBLIC_HTTP"))
	if value == "" {
		return false, nil
	}
	allowed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("VEIL_UNSAFE_ALLOW_PUBLIC_HTTP must be a boolean: %w", err)
	}
	return allowed, nil
}

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
		if goos == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			autoTLSDir = filepath.Join(pd, "Veil", "autocert")
		} else {
			autoTLSDir = "/var/lib/veil/autocert"
		}
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

// goos is overridable in tests to exercise Windows-specific path defaults
// without requiring a Windows build.
var goos = runtime.GOOS

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
