package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/veil-panel/veil/internal/installer"
)

type ServerInfo struct {
	Version   string
	Mode      string
	AuthToken string
	StatePath string
	ApplyRoot string
	KeyPath   string
}

type StatusResponse struct {
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

type ServiceRuntimeStatus struct {
	Unit        string
	LoadState   string
	ActiveState string
	SubState    string
	Error       string
}

var serviceStatusReader = readSystemdServiceStatus

type RURecommendedPreviewRequest struct {
	Domain string `json:"domain"`
	Email  string `json:"email"`
	Stack  string `json:"stack,omitempty"`
}

type RURecommendedPreviewResponse struct {
	Domain             string `json:"domain"`
	Email              string `json:"email"`
	Stack              string `json:"stack"`
	Port               int    `json:"port"`
	NaiveClientURL     string `json:"naiveClientURL"`
	Hysteria2ClientURI string `json:"hysteria2ClientURI"`
	Caddyfile          string `json:"caddyfile"`
	Hysteria2YAML      string `json:"hysteria2YAML"`
}

func NewRouter(info ServerInfo) (http.Handler, Reloader) {
	mux := http.NewServeMux()
	state := newManagementState(info)
	metrics := NewMetricsCollector()
	mux.HandleFunc("/metrics", metrics.ServeHTTP)
	state.register(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeNotFound(w)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet, http.MethodHead)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Origin-Agent-Cluster", "?1")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(panelHTML))
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet, http.MethodHead)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet {
			if info.StatePath != "" {
				if _, err := os.Stat(info.StatePath); err != nil {
					writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{
						"status": "unhealthy",
						"error":  err.Error(),
					})
					return
				}
			}
			writeJSON(w, map[string]string{"status": "ok"})
		}
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet, http.MethodHead)
			return
		}
		setJSONHeaders(w)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(StatusResponse{
				SchemaVersion: "v1",
				Name:          "Veil",
				Version:       info.Version,
				Mode:          info.Mode,
				Services:      buildServiceStatuses(),
			})
		}
	})
	mux.HandleFunc("/api/tools/speedtest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := validateEmptyJSONBody(r); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := speedtestRunner(r)
		if err != nil {
			writeError(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/profiles/ru-recommended/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var req RURecommendedPreviewRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
			Domain: req.Domain,
			Email:  req.Email,
			Stack:  installer.Stack(req.Stack),
			Availability: installer.PortAvailability{
				TCPBusy: map[int]bool{},
				UDPBusy: map[int]bool{},
			},
			Secret:     func(label string) string { return "preview-" + label },
			RandomPort: func() int { return 31874 },
		})
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, RURecommendedPreviewResponse{
			Domain:             profile.Domain,
			Email:              profile.Email,
			Stack:              string(profile.Stack),
			Port:               profile.PortPlan.Port,
			NaiveClientURL:     redactProfileSecrets(profile, profile.NaiveClientURL),
			Hysteria2ClientURI: redactProfileSecrets(profile, profile.Hysteria2ClientURI),
			Caddyfile:          redactProfileSecrets(profile, profile.Caddyfile),
			Hysteria2YAML:      redactProfileSecrets(profile, profile.Hysteria2YAML),
		})
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		unit := r.URL.Query().Get("unit")
		if unit == "" {
			unit = "veil"
		}
		if !validLogUnit(unit) {
			writeError(w, "invalid unit name", http.StatusBadRequest)
			return
		}
		lines := 50
		if ls := r.URL.Query().Get("lines"); ls != "" {
			n, err := strconv.Atoi(ls)
			if err != nil || n < 1 || n > 500 {
				writeError(w, "lines must be 1-500", http.StatusBadRequest)
				return
			}
			lines = n
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx,
			"journalctl", "-u", unit+".service", "--no-pager", "-n", strconv.Itoa(lines), "-o", "short-iso",
		).CombinedOutput()
		if err != nil {
			writeError(w, "failed to read logs: "+strings.TrimSpace(string(out)), http.StatusServiceUnavailable)
			return
		}
		result := map[string]string{
			"unit":   unit,
			"output": string(out),
		}
		writeJSON(w, result)
	})
	rateLimited := rateLimitMiddleware(metrics, mux)
	authenticated := authMiddleware(info.AuthToken, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	return metrics.MetricsMiddleware(secured), state
}

// securityHeadersMiddleware adds baseline security headers to every response.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("X-DNS-Prefetch-Control", "off")
		w.Header()["Server"] = nil // hide Go version from Server header
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(metrics *MetricsCollector, next http.Handler) http.Handler {
	limiter := NewRateLimiter(100, 20) // 100 req/min per IP, burst 20
	limiter.SetEndpointLimits(map[string]EndpointLimit{
		"/api/tools/speedtest": {RatePerMinute: 2, Burst: 1},    // 1 req/30s
		"/api/logs":            {RatePerMinute: 10, Burst: 3},   // 10 req/min for log reads
	})
	limiter.onRateLimited = func() { metrics.TrackRateLimitHit() }
	return limiter.Middleware(next)
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	for _, secret := range []string{profile.NaivePassword, profile.Hysteria2Password, profile.PanelAuthToken} {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func buildServiceStatuses() []ServiceStatus {
	services := []ServiceStatus{
		{Name: "veil", Managed: true, Unit: "veil.service"},
		{Name: "naive", Managed: true, Transport: "tcp", Unit: "caddy.service"},
		{Name: "hysteria2", Managed: true, Transport: "udp", Unit: "hysteria2.service"},
		{Name: "sing-box", Managed: true, Unit: "sing-box.service"},
	}
	for i := range services {
		runtime := serviceStatusReader(services[i].Unit)
		services[i].LoadState = runtime.LoadState
		services[i].ActiveState = runtime.ActiveState
		services[i].SubState = runtime.SubState
		services[i].Error = runtime.Error
	}
	return services
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	status := ServiceRuntimeStatus{Unit: unit, LoadState: "unknown", ActiveState: "unknown", SubState: "unknown"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx,
		"systemctl",
		"show",
		unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--no-page",
	).CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			if value != "" {
				status.LoadState = value
			}
		case "ActiveState":
			if value != "" {
				status.ActiveState = value
			}
		case "SubState":
			if value != "" {
				status.SubState = value
			}
		}
	}
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}

func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !validAuthToken(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Veil API"`)
			writeError(w, "missing or invalid API token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validAuthToken(r *http.Request, want string) bool {
	provided := r.Header.Get("X-Veil-Token")
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(want)) == 1
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	const scheme = "Bearer "
	if len(header) <= len(scheme) {
		return ""
	}
	if !strings.EqualFold(header[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(header[len(scheme):])
}

const maxJSONBodyBytes int64 = 1024 * 1024

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	ct := r.Header.Get("Content-Type")
	if ct != "" && ct != "application/json" {
		writeError(w, "Unsupported Media Type: Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return false
		}
		writeError(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			writeError(w, "request body must contain a single JSON value", http.StatusBadRequest)
			return false
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		writeError(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	setJSONHeaders(w)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

func setJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	setJSONHeaders(w)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSONStatus: encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, msg, code)
}

func writeNotFound(w http.ResponseWriter) {
	writeError(w, "404 page not found", http.StatusNotFound)
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, "method not allowed", http.StatusMethodNotAllowed)
}

// validateEmptyJSONBody validates Content-Type and body size for POST endpoints
// that expect no meaningful body (like speedtest). If Content-Type is set, it must
// be application/json; if a body is present, it must be empty or "{}".
func validateEmptyJSONBody(r *http.Request) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if ct != "application/json" {
			return fmt.Errorf("Content-Type must be application/json")
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes))
	if err != nil {
		return fmt.Errorf("request body too large")
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" && trimmed != "{}" {
		return fmt.Errorf("unexpected request body")
	}
	return nil
}

// validLogUnit checks that a systemd unit name contains only safe characters.
func validLogUnit(unit string) bool {
	if unit == "" {
		return false
	}
	for _, r := range unit {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '@' || r == '.') {
			return false
		}
	}
	return true
}
