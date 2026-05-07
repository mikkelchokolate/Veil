package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"
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

func NewRouter(info ServerInfo) (http.Handler, Reloader) {
	return NewRouterComposition(info).Build()
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

func rateLimitMiddleware(metrics *MetricsCollector, next http.Handler) http.Handler {
	limiter := DefaultRateLimitPolicy().NewLimiter()
	limiter.onRateLimited = func() { metrics.TrackRateLimitHit() }
	return limiter.Middleware(next)
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
func runtimeInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
