package status

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

type Options struct {
	Listen      string
	AuthToken   string
	WebBasePath string
	JSON        bool
}

type Response struct {
	SchemaVersion string          `json:"schemaVersion"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Mode          string          `json:"mode"`
	Services      []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	Managed     bool   `json:"managed"`
	Transport   string `json:"transport,omitempty"`
	Unit        string `json:"unit,omitempty"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
}

type AuthTokenResolver func(string) (string, string)

type Query struct {
	opts        Options
	out         io.Writer
	resolveAuth AuthTokenResolver
}

func NewQuery(opts Options, out io.Writer, resolveAuth AuthTokenResolver) Query {
	if resolveAuth == nil {
		resolveAuth = func(token string) (string, string) { return token, "flag" }
	}
	return Query{opts: opts, out: out, resolveAuth: resolveAuth}
}

func ResolveWebBasePath(flagValue string) string {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_WEB_BASE_PATH")); path != "" {
		return path
	}
	if path := strings.TrimSpace(installedEnvValue("VEIL_WEB_BASE_PATH")); path != "" {
		return path
	}
	return "/"
}

func ResolveAuthToken(flagValue string) string {
	if token := strings.TrimSpace(flagValue); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("VEIL_API_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(installedEnvValue("VEIL_API_TOKEN"))
}

func (q Query) Run(ctx context.Context) error {
	addr := ResolveListen(q.opts.Listen)
	candidates := CandidateAddrs(addr)
	basePath, err := webbasepath.Normalize(ResolveWebBasePath(q.opts.WebBasePath))
	if err != nil {
		return fmt.Errorf("invalid web base path: %w", err)
	}
	token, _ := q.resolveAuth(q.opts.AuthToken)
	if strings.TrimSpace(token) == "" {
		token = ResolveAuthToken("")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var lastErr error
	for _, candidate := range candidates {
		status, err := Fetch(ctx, strings.TrimRight(candidate, "/")+basePath+"api/status", token)
		if err == nil {
			return q.Render(status)
		}
		lastErr = err
	}
	return fmt.Errorf("fetch status from %s: %w", strings.Join(candidates, ", "), lastErr)
}

const DefaultListen = "127.0.0.1:2096"

var (
	installedEnvFile   string
	installedStateFile string
	panelTLSCertFile   string
	// systemdSystemDir is a test seam: production scans the real systemd
	// system directory for the installed veil.service drop-in that records a
	// custom EnvironmentFile (issue #635).
	systemdSystemDir = "/etc/systemd/system"
)

func ResolveListen(flagValue string) string {
	addr := strings.TrimSpace(flagValue)
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv("VEIL_LISTEN"))
	}
	if addr == "" {
		addr = strings.TrimSpace(installedEnvValue("VEIL_LISTEN"))
	}
	if addr == "" {
		addr = strings.TrimSpace(installedPanelListen())
	}
	if addr == "" {
		addr = DefaultListen
	}
	return normalizeProbeAddr(addr)
}

func effectiveEnvFile() string {
	if installedEnvFile != "" {
		return installedEnvFile
	}
	candidates := envFileCandidates()
	for _, candidate := range candidates[:len(candidates)-1] {
		if fileExists(candidate) {
			return candidate
		}
	}
	// Nothing found: return the last (most generic) candidate so error paths
	// still report a meaningful location.
	return candidates[len(candidates)-1]
}

// envFileCandidates lists the locations a `veil status` probe may find the
// installed EnvironmentFile, most specific first. VEIL_ETC_DIR is explicit
// operator intent, so it short-circuits discovery; otherwise the root implied
// by VEIL_LIVE_ROOT/VEIL_KEY_PATH, the EnvironmentFile recorded in the
// installed systemd unit (custom --etc-dir installs), and finally the
// packaged path are tried in order (issue #635).
func envFileCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{defaultVeilEnvPath()}
	}
	if dir := strings.TrimSpace(os.Getenv("VEIL_ETC_DIR")); dir != "" {
		return []string{filepath.Join(dir, "veil.env")}
	}
	candidates := []string{}
	if etc := hostenv.EtcDir(); etc != hostenv.DefaultEtcDir {
		candidates = append(candidates, filepath.Join(etc, "veil.env"))
	}
	if path := systemdEnvFilePath(); path != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, defaultVeilEnvPath())
	return candidates
}

// systemdEnvFilePath scans the installed veil.service drop-ins for an
// EnvironmentFile override. Later drop-ins win under systemd semantics, so
// the last assignment is used.
func systemdEnvFilePath() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	dropInDir := filepath.Join(systemdSystemDir, "veil.service.d")
	entries, err := os.ReadDir(dropInDir)
	if err != nil {
		return ""
	}
	found := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dropInDir, entry.Name()))
		if err != nil {
			continue
		}
		if path := envFileDirective(body); path != "" {
			found = path
		}
	}
	return found
}

// envFileDirective extracts the EnvironmentFile= target from a systemd unit
// fragment. A leading "-" (ignore-missing marker) and systemd-style quoting
// are stripped.
func envFileDirective(body []byte) string {
	found := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != "EnvironmentFile" {
			continue
		}
		// systemd applies every EnvironmentFile= line in order and an empty
		// assignment resets the list; keep the last directive, matching the
		// last-drop-in-wins ordering used across .conf fragments.
		value = strings.TrimPrefix(strings.TrimSpace(value), "-")
		found = strings.Trim(value, `"'`)
	}
	return found
}

func effectiveStateFile() string {
	if installedStateFile != "" {
		return installedStateFile
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path
	}
	if dir := strings.TrimSpace(os.Getenv("VEIL_VAR_DIR")); dir != "" {
		return filepath.Join(dir, "state.json")
	}
	candidates := stateFileCandidates()
	for _, candidate := range candidates[:len(candidates)-1] {
		if fileExists(candidate) {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

// stateFileCandidates lists where the installed management state may live:
// the VEIL_STATE_PATH recorded in the discovered env file first (it is the
// authoritative installed location), then the VEIL_VAR_DIR-implied root, then
// the packaged default (issue #635).
func stateFileCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{defaultStatePath()}
	}
	candidates := []string{}
	if path := strings.TrimSpace(installedEnvValue("VEIL_STATE_PATH")); path != "" {
		candidates = append(candidates, path)
	}
	if dir := hostenv.VarDir(); dir != hostenv.DefaultVarDir {
		candidates = append(candidates, filepath.Join(dir, "state.json"))
	}
	candidates = append(candidates, defaultStatePath())
	return candidates
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func defaultVeilEnvPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsProgramData(), "Veil", "veil.env")
	}
	return "/etc/veil/veil.env"
}

func defaultStatePath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsProgramData(), "Veil", "state.json")
	}
	return "/var/lib/veil/state.json"
}

func windowsProgramData() string {
	if pd := strings.TrimSpace(os.Getenv("ProgramData")); pd != "" {
		return pd
	}
	return `C:\ProgramData`
}

func installedEnvValue(key string) string {
	body, err := os.ReadFile(effectiveEnvFile())
	if err != nil {
		return ""
	}
	return envFileValue(body, key)
}

func envFileValue(body []byte, key string) string {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return ""
}

func installedPanelListen() string {
	body, err := os.ReadFile(effectiveStateFile())
	if err != nil {
		return ""
	}
	var snapshot struct {
		Settings struct {
			PanelListen string `json:"panelListen"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return ""
	}
	return snapshot.Settings.PanelListen
}

func normalizeProbeAddr(addr string) string {
	if strings.Contains(addr, "://") {
		parsed, err := url.Parse(addr)
		if err != nil || parsed.Host == "" {
			return addr
		}
		host, port, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			return addr
		}
		parsed.Host = net.JoinHostPort(rewriteUnspecifiedHost(host), port)
		return parsed.String()
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return net.JoinHostPort(rewriteUnspecifiedHost(host), port)
}

func rewriteUnspecifiedHost(host string) string {
	trimmed := strings.Trim(host, "[]")
	if trimmed == "" || trimmed == "*" || trimmed == "0.0.0.0" {
		return "127.0.0.1"
	}
	ip := net.ParseIP(trimmed)
	if ip != nil && ip.IsUnspecified() {
		if ip.To4() == nil {
			return "::1"
		}
		return "127.0.0.1"
	}
	return host
}

func CandidateAddrs(addr string) []string {
	if strings.Contains(addr, "://") {
		return []string{addr}
	}
	return []string{"https://" + addr, "http://" + addr}
}

func (q Query) Render(status *Response) error {
	if q.opts.JSON {
		enc := json.NewEncoder(q.out)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	fmt.Fprintf(q.out, "Veil %s\n", status.Version)
	fmt.Fprintf(q.out, "Mode: %s\n", status.Mode)
	fmt.Fprintln(q.out, "Services:")
	for _, svc := range status.Services {
		state := svc.ActiveState
		if svc.Error != "" {
			state = fmt.Sprintf("%s (error: %s)", state, svc.Error)
		}
		marker := "○"
		if svc.ActiveState == "active" {
			marker = "●"
		} else if svc.ActiveState == "failed" {
			marker = "✕"
		}
		proto := ""
		if svc.Transport != "" {
			proto = fmt.Sprintf(" (%s)", svc.Transport)
		}
		fmt.Fprintf(q.out, "  %s %s%s: %s\n", marker, svc.Name, proto, state)
	}
	return nil
}

func Fetch(ctx context.Context, url string, token string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("X-Veil-Token", token)
	}
	resp, err := HTTPClient(url).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var status Response
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

var HTTPClient = defaultHTTPClient

func defaultHTTPClient(rawURL string) *http.Client {
	if !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return http.DefaultClient
	}
	certPath := effectivePanelTLSCertFile()
	pem, err := os.ReadFile(certPath)
	if err != nil {
		return http.DefaultClient
	}
	pool, poolErr := x509.SystemCertPool()
	if poolErr != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return http.DefaultClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	return &http.Client{Transport: transport}
}

func effectivePanelTLSCertFile() string {
	if path := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")); path != "" {
		return path
	}
	if path := strings.TrimSpace(installedEnvValue("VEIL_TLS_CERT")); path != "" {
		return path
	}
	if panelTLSCertFile != "" {
		return panelTLSCertFile
	}
	if runtime.GOOS != "windows" {
		// A custom --etc-dir install stores the panel TLS pair under
		// <etc>/panel; probe it before falling back to the packaged path
		// (issue #635).
		if dir := strings.TrimSpace(os.Getenv("VEIL_ETC_DIR")); dir != "" {
			return filepath.Join(dir, "panel", "tls.crt")
		}
		if etc := hostenv.EtcDir(); etc != hostenv.DefaultEtcDir {
			candidate := filepath.Join(etc, "panel", "tls.crt")
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	return defaultPanelTLSCertPath()
}

func defaultPanelTLSCertPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsProgramData(), "Veil", "panel", "tls.crt")
	}
	return "/etc/veil/panel/tls.crt"
}
