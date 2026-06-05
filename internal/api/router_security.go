package api

import (
	"context"
	"crypto/subtle"
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
		w.Header()["Server"] = nil // hide Go version from Server header
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
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

		requiresAuth := strings.HasPrefix(path, "/api/")
		if path == "/api/auth/login" ||
			path == "/api/auth/logout" ||
			path == "/api/auth/status" {
			requiresAuth = false
		}
		if opts.AllowSetup && (path == "/api/setup/status" || path == "/api/setup/complete") {
			requiresAuth = false
		}
		if path == "/healthz" && opts.ProtectHealthz {
			requiresAuth = true
		}
		if path == "/metrics" && opts.ProtectMetrics {
			requiresAuth = true
		}
		if !requiresAuth {
			next.ServeHTTP(w, r)
			return
		}

		var username string
		var role string
		var isCookieSession bool

		// 1. Check static token authentication (X-Veil-Token / Authorization Bearer)
		hasStaticToken := false
		if opts.Token != "" && validAuthToken(r, opts.Token) {
			username = "api-token"
			role = "admin"
			hasStaticToken = true
		}

		// 2. If no static token, check cookie session
		if !hasStaticToken {
			cookie, err := r.Cookie("veil_session")
			if err == nil {
				if sess, ok := state.sessionRegistry().Get(cookie.Value); ok {
					username = sess.Username
					role = sess.Role
					isCookieSession = true
				}
			}
		}

		// 3. Fallback check: if there are no registered users in state, and token is empty, we allow access
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

		// 4. CSRF check for mutating cookie sessions
		if isCookieSession && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete) {
			providedCSRF := r.Header.Get("X-CSRF-Token")
			if !state.sessionRegistry().ValidateCSRF(currentSessionToken(r), providedCSRF) {
				writeError(w, "invalid or missing CSRF token", http.StatusForbidden)
				return
			}
		}

		// 5. RBAC check for mutating operations
		if role != "admin" && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete) {
			writeError(w, "forbidden: admin role required", http.StatusForbidden)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, contextKeyUsername, username)
		ctx = context.WithValue(ctx, contextKeyRole, role)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
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
