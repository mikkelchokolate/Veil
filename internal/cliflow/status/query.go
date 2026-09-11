package status

import (
	"context"
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

func (q Query) Run(ctx context.Context) error {
	addr := ResolveListen(q.opts.Listen)
	candidates := CandidateAddrs(addr)
	basePath, err := webbasepath.Normalize(q.opts.WebBasePath)
	if err != nil {
		return fmt.Errorf("invalid web base path: %w", err)
	}
	token, _ := q.resolveAuth(q.opts.AuthToken)
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
	return localProbeAddr(addr)
}

func effectiveEnvFile() string {
	if installedEnvFile != "" {
		return installedEnvFile
	}
	return defaultVeilEnvPath()
}

func effectiveStateFile() string {
	if installedStateFile != "" {
		return installedStateFile
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path
	}
	return defaultStatePath()
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

func localProbeAddr(addr string) string {
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

var HTTPClient = func(rawURL string) *http.Client { return http.DefaultClient }
