package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// securityHeadersMiddleware adds baseline security headers to every response.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("X-DNS-Prefetch-Control", "off")
		w.Header()["Server"] = nil
		if r.TLS != nil {
			host, _, _ := net.SplitHostPort(r.Host)
			if host == "" {
				host = r.Host
			}
			if net.ParseIP(host) == nil {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
		}
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(state *managementState, token string, next http.Handler) http.Handler {
	return authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             token,
		AllowDevAnonymous: true,
	}, next)
}

type authMiddlewareOptions struct {
	Token             string
	ProtectHealthz    bool
	ProtectMetrics    bool
	AllowDevAnonymous bool
	AllowSetup        bool
}

func authMiddlewareWithOptions(state *managementState, opts authMiddlewareOptions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		state.mu.Lock()
		startupStateLoadFailed := state.startupStateLoadFailed
		startupStateLoadErr := state.startupStateLoadErr
		state.mu.Unlock()
		if startupStateLoadFailed && strings.HasPrefix(path, "/api/") {
			if privilegedHelperSocketUnavailable(startupStateLoadErr) {
				writePrivilegedError(w, startupStateLoadErr)
				return
			}
			writeError(w, "management state unavailable", http.StatusServiceUnavailable)
			return
		}
		capability, known := capabilityForEndpoint(r.Method, path)
		if !known {
			writeError(w, "endpoint authorization policy is not defined", http.StatusNotFound)
			return
		}
		if path == "/healthz" && opts.ProtectHealthz {
			capability = capabilityViewer
		}
		if path == "/metrics" && opts.ProtectMetrics {
			capability = capabilityViewer
		}
		if path == "/api/setup/complete" && !opts.AllowSetup {
			capability = capabilityAdminMutation
		}
		if capability == capabilityPublic {
			next.ServeHTTP(w, r)
			return
		}

		var username string
		var role string
		var isCookieSession bool
		var sessionToken string

		hasStaticToken := false
		if opts.Token != "" && validAuthToken(r, opts.Token) {
			username = "api-token"
			role = "admin"
			hasStaticToken = true
		}

		if !hasStaticToken {
			cookie, err := r.Cookie("veil_session")
			if err == nil {
				sessionToken = cookie.Value
				if sess, ok := state.sessionRegistry().Get(cookie.Value); ok {
					username = sess.Username
					role = sess.Role
					isCookieSession = true
				}
			}
		}

		if isCookieSession {
			state.mu.Lock()
			matched := false
			for _, user := range state.users {
				if user.Username == username {
					role = user.Role
					matched = true
					break
				}
			}
			state.mu.Unlock()
			if !matched {
				state.sessionRegistry().Delete(sessionToken)
				username, role, isCookieSession = "", "", false
			}
		}

		if username == "" {
			state.mu.Lock()
			noUsers := len(state.users) == 0
			state.mu.Unlock()
			if opts.AllowDevAnonymous && noUsers && opts.Token == "" {
				username = "dev-anonymous"
				role = "admin"
			}
		}

		if username == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Veil API"`)
			writeError(w, "missing or invalid API token or session", http.StatusUnauthorized)
			return
		}

		if isCookieSession && isMutatingRequest(r) {
			providedCSRF := r.Header.Get("X-CSRF-Token")
			if !state.sessionRegistry().ValidateCSRF(currentSessionToken(r), providedCSRF) {
				writeError(w, "invalid or missing CSRF token", http.StatusForbidden)
				return
			}
		}
		if !capabilityAllowsRole(capability, role) {
			writeError(w, "forbidden: endpoint capability requires admin role", http.StatusForbidden)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, contextKeyUsername, username)
		ctx = context.WithValue(ctx, contextKeyRole, role)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

func isMutatingRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isSelfServiceMutation(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/auth/locale"
}

func isReadOnlyDiagnosticRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/tools/dns-lookup",
		"/api/tools/ping",
		"/api/tools/speedtest",
		"/api/apply/plan",
		"/api/client-links/qr",
		"/api/profiles/ru-recommended/preview":
		return true
	default:
		return false
	}
}

type contextKey string

const (
	contextKeyUsername contextKey = "username"
	contextKeyRole     contextKey = "role"
)

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
