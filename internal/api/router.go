package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/observability"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type ConfigurationValidator interface {
	Validate(context.Context, livevalidation.Request) livevalidation.Response
}

type ServerInfo struct {
	Version                 string
	Mode                    string
	AuthToken               string
	PublicListen            bool
	MetricsAuthRequired     bool
	StatePath               string
	ApplyRoot               string
	LiveRoot                string
	KeyPath                 string
	PanelListen             string
	PanelAccess             string
	Domain                  string
	Email                   string
	WebBasePath             string
	SetupAllowed            bool
	ConfigurationValidator  ConfigurationValidator
	Privileged              privileged.Client
	RequirePrivilegedHelper bool
	UpdateStager            func(context.Context) (string, error)
}

func NewRouter(info ServerInfo) (http.Handler, Reloader) {
	return NewRouterComposition(info).Build()
}

// stripBasePathMiddleware removes the base path prefix from request URL before routing.
func stripBasePathMiddleware(prefix string, next http.Handler) http.Handler {
	mountPath := strings.TrimSuffix(prefix, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match either the mount itself or a child path. A raw HasPrefix check would
		// incorrectly accept /secretary when the configured mount is /secret.
		if r.URL.Path != mountPath && !strings.HasPrefix(r.URL.Path, mountPath+"/") {
			writeNotFound(w)
			return
		}

		stripped := strings.TrimPrefix(r.URL.Path, mountPath)
		if stripped == "" {
			stripped = "/"
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = stripped
		next.ServeHTTP(w, r2)
	})
}

func rateLimitMiddleware(metrics *observability.MetricsCollector, next http.Handler) http.Handler {
	limiter := observability.DefaultRateLimitPolicy().NewLimiter()
	limiter.SetOnRateLimited(func() { metrics.TrackRateLimitHit() })
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"message": msg}); err != nil {
		log.Printf("writeError: encode error: %v", err)
	}
}

func writePrivilegedError(w http.ResponseWriter, err error) {
	if privilegedHelperSocketUnavailable(err) {
		writePrivilegedHelperUnavailable(w)
		return
	}
	status := http.StatusInternalServerError
	code := privileged.ErrorOperationFailed
	message := "privileged operation failed"
	var operationError *privileged.Error
	if errors.As(err, &operationError) {
		code = operationError.Code
		message = operationError.Message
		switch operationError.Code {
		case privileged.ErrorInvalidRequest:
			status = http.StatusBadRequest
		case privileged.ErrorForbiddenOperation:
			status = http.StatusForbidden
		case privileged.ErrorNotFound:
			status = http.StatusNotFound
		case privileged.ErrorConflict:
			status = http.StatusConflict
		}
	}
	if strings.Contains(strings.ToLower(message), "backup passphrase") {
		status = http.StatusServiceUnavailable
	}
	writeJSONStatus(w, status, map[string]any{
		"error": map[string]string{
			"code":    string(code),
			"message": message,
		},
	})
}

func privilegedHelperSocketUnavailable(err error) bool {
	var operationError *privileged.Error
	if !errors.As(err, &operationError) {
		return false
	}
	message := strings.ToLower(operationError.Message)
	if !strings.Contains(message, "helper.sock") {
		return false
	}
	for _, marker := range []string{"no such file or directory", "connection refused", "permission denied", "dead network"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
		return err
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 || string(body) == "{}" {
		return nil
	}
	return fmt.Errorf("request body must be empty or {}")
}

func runtimeInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
