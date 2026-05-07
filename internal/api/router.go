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
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type ServerInfo struct {
	Version     string
	Mode        string
	AuthToken   string
	StatePath   string
	ApplyRoot   string
	KeyPath     string
	WebBasePath string
}

var firewallStatusReader = readFirewallStatus

func NewRouter(info ServerInfo) (http.Handler, Reloader) {
	mux := http.NewServeMux()
	state := newManagementState(info)
	metrics := NewMetricsCollector()
	basePath := info.WebBasePath
	if basePath == "" {
		basePath = "/"
	}
	mux.HandleFunc("/metrics", metrics.ServeHTTP)
	RuntimeRoutes{}.Register(mux)
	ServiceActionRoutes{}.Register(mux)
	state.register(mux)
	PanelRoutes{Info: info, BasePath: basePath}.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{}.Register(mux)
	// Wrap the mux to strip the web base path prefix from incoming requests.
	var handler http.Handler = mux
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, mux)
	}
	rateLimited := rateLimitMiddleware(metrics, handler)
	authenticated := authMiddleware(info.AuthToken, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	return metrics.MetricsMiddleware(secured), state
}

// stripBasePathMiddleware removes the base path prefix from request URL before routing.
func stripBasePathMiddleware(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix[:len(prefix)-1]) {
			// Strip prefix: /secret/foo → /foo
			stripped := strings.TrimPrefix(r.URL.Path, prefix[:len(prefix)-1])
			if stripped == "" {
				stripped = "/"
			}
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = new(url.URL)
			*r2.URL = *r.URL
			r2.URL.Path = stripped
			next.ServeHTTP(w, r2)
			return
		}
		// Path doesn't match base path — reject with 404.
		writeNotFound(w)
	})
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
		"/api/tools/speedtest":  {RatePerMinute: 2, Burst: 1},  // 1 req/30s
		"/api/tools/dns-lookup": {RatePerMinute: 10, Burst: 3}, // 10 req/min for DNS lookups
		"/api/tools/ping":       {RatePerMinute: 5, Burst: 2},  // 5 req/min for ping
		"/api/logs":             {RatePerMinute: 10, Burst: 3}, // 10 req/min for log reads
	})
	limiter.onRateLimited = func() { metrics.TrackRateLimitHit() }
	return limiter.Middleware(next)
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
func readFirewallStatus() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ufw", "status").CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(output), "Status: active"), nil
}

func runtimeInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
