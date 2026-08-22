package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func ResolveListen(flagValue string) string {
	if addr := strings.TrimSpace(flagValue); addr != "" {
		return addr
	}
	return "127.0.0.1:2096"
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
