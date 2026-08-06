package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/mikkelchokolate/Veil/internal/clientaddr"
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
	TrustedProxyCIDRs       []string
	PasswordHasher          PasswordHasher
	DatabaseOpener          func(string) (*sql.DB, error)
}

func NewRouter(info ServerInfo) (http.Handler, Reloader) {
	return NewRouterComposition(info).Build()
}

type clientRequestIDContextKey struct{}

var requestIDFallbackCounter atomic.Uint64

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientRequestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if len(clientRequestID) > 128 {
			clientRequestID = clientRequestID[:128]
		}
		for _, char := range clientRequestID {
			if char < 0x20 || char > 0x7e {
				clientRequestID = ""
				break
			}
		}
		requestID := newServerRequestID()
		request := r.Clone(context.WithValue(r.Context(), clientRequestIDContextKey{}, clientRequestID))
		request.Header = r.Header.Clone()
		request.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, request)
	})
}

func newServerRequestID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("fallback-%016x", requestIDFallbackCounter.Add(1))
}

func clientProvidedRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	value, _ := r.Context().Value(clientRequestIDContextKey{}).(string)
	return value
}

// stripBasePathMiddleware removes the base path prefix from request URL before routing.
func stripBasePathMiddleware(prefix string, next http.Handler) http.Handler {
	mountPath := strings.TrimSuffix(prefix, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The public token-based subscription endpoint (/s/{token}) must stay
		// reachable even when the panel is mounted under a secret base path:
		// the unguessable token is the capability, not the panel path.
		if strings.HasPrefix(r.URL.Path, "/s/") {
			next.ServeHTTP(w, r)
			return
		}
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

func newRateLimitMiddleware(metrics *observability.MetricsCollector, trustedProxyCIDRs []string, next http.Handler) (http.Handler, *observability.RateLimiter) {
	resolver, err := clientaddr.New(trustedProxyCIDRs)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, "invalid trusted proxy configuration", http.StatusInternalServerError)
		}), nil
	}
	limiter := observability.DefaultRateLimitPolicy().NewLimiter()
	limiter.SetClientAddressResolver(resolver)
	limiter.SetOnRateLimited(func() { metrics.TrackRateLimitHit() })
	return limiter.Middleware(next), limiter
}

const maxJSONBodyBytes int64 = 1024 * 1024

var (
	errUnsupportedJSONMediaType = errors.New("Content-Type must be application/json")
	errJSONBodyTooLarge         = errors.New("request body too large")
)

func isJSONMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !isJSONMediaType(ct) {
		writeError(w, "Unsupported Media Type: Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, errJSONBodyTooLarge.Error(), http.StatusRequestEntityTooLarge)
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
			writeError(w, errJSONBodyTooLarge.Error(), http.StatusRequestEntityTooLarge)
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
	code = canonicalRequestErrorStatus(msg, code)
	errorCode := apiErrorCode(code)
	if code >= http.StatusInternalServerError {
		msg = "internal server error"
	}
	writeErrorEnvelope(w, errorCode, msg, code)
}

func writeErrorEnvelope(w http.ResponseWriter, errorCode, message string, status int) {
	setJSONHeaders(w)
	requestID := w.Header().Get("X-Request-ID")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":      errorCode,
			"message":   message,
			"requestId": requestID,
		},
	}); err != nil {
		log.Printf("writeError: encode error: %v", err)
	}
}

func apiErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType, http.StatusRequestEntityTooLarge:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout:
		return "request_timeout"
	case http.StatusUnprocessableEntity:
		return "validation_failed"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "dependency_unavailable"
	default:
		return "internal_error"
	}
}

func canonicalRequestErrorStatus(message string, status int) int {
	if status != http.StatusBadRequest {
		return status
	}
	switch message {
	case errUnsupportedJSONMediaType.Error():
		return http.StatusUnsupportedMediaType
	case errJSONBodyTooLarge.Error():
		return http.StatusRequestEntityTooLarge
	default:
		return status
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
	if status >= http.StatusInternalServerError && message != "privileged helper is unavailable" {
		message = "privileged operation failed"
	}
	writeErrorEnvelope(w, string(code), message, status)
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
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONMediaType(ct) {
		return errUnsupportedJSONMediaType
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return errJSONBodyTooLarge
		}
		return err
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 || string(body) == "{}" {
		return nil
	}
	return errors.New("request body must be empty or {}")
}

func runtimeInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
