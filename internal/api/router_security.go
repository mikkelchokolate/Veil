package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// securityHeadersMiddleware adds baseline security headers to every response.
// tlsEdge reports whether the process sits behind a TLS-terminating edge that
// Veil itself rendered (panelAccess=caddy): the inner listener then sees plain
// loopback HTTP (r.TLS == nil) even though clients only ever reach the panel
// over HTTPS, so HSTS must be emitted for the edge the way Secure cookies
// already are (#902). Browsers ignore HSTS received over plain HTTP, so the
// header is harmless on any residual direct-HTTP path.
func securityHeadersMiddleware(next http.Handler, tlsEdge bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("X-DNS-Prefetch-Control", "off")
		w.Header()["Server"] = nil
		if r.TLS != nil || tlsEdge {
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
		if r.Method == http.MethodOptions {
			// Never dispatch OPTIONS to an application handler: classifying it
			// as public handed every /api handler's method switch to anonymous
			// callers, leaking the protocol catalog and a 404-vs-405 existence
			// oracle (#1106). Answer with the methods registered for the path;
			// an API or subscription path with no registered method fails
			// closed like every other unregistered route.
			if methods := registeredEndpointMethods(path); methods != nil {
				w.Header().Set("Allow", strings.Join(append(methods, http.MethodOptions), ", "))
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if path == "/api" || path == "/s" || strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/s/") {
				writeError(w, "endpoint authorization policy is not defined", http.StatusNotFound)
				return
			}
		}
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
		if opts.ProtectHealthz && isHealthProbePath(path) {
			capability = capabilityViewer
		}
		if path == "/metrics" && opts.ProtectMetrics {
			capability = capabilityViewer
		}
		if path == "/api/setup/complete" && !opts.AllowSetup {
			capability = capabilityAdminMutation
		}
		if capability == capabilityPublic {
			// Public endpoints short-circuit auth, but a mutating public
			// request that arrives with a LIVE cookie session must still
			// prove CSRF (audit #198: logout was revocable cross-site).
			// Login and first-run setup replace or create credentials while
			// the SPA may have dropped CSRF after a failed status refresh.
			if isMutatingRequest(r) && !csrfExemptPublicMutation(path) {
				if cookie, err := r.Cookie("veil_session"); err == nil {
					if _, ok := state.sessionRegistry().Get(cookie.Value); ok {
						providedCSRF := r.Header.Get("X-CSRF-Token")
						if !state.sessionRegistry().ValidateCSRF(cookie.Value, providedCSRF) {
							writeError(w, "invalid or missing CSRF token", http.StatusForbidden)
							return
						}
					}
				}
			}
			next.ServeHTTP(w, r)
			return
		}

		var username string
		var role string
		var isCookieSession bool
		var sessionToken string
		var sessionBootstrap bool
		var sessionSecondFactor bool

		hasStaticToken := false
		if opts.Token != "" && validAuthToken(r, opts.Token) {
			username = "api-token"
			role = "admin"
			hasStaticToken = true
		}

		if !hasStaticToken {
			cookie, err := r.Cookie("veil_session")
			if err == nil {
				if healthErr := state.sessionRegistry().Healthy(); healthErr != nil {
					writeError(w, "session storage unavailable", http.StatusServiceUnavailable)
					return
				}
				sessionToken = cookie.Value
				if sess, ok := state.sessionRegistry().Get(cookie.Value); ok {
					username = sess.Username
					role = sess.Role
					isCookieSession = true
					sessionBootstrap = sess.Bootstrap
					sessionSecondFactor = sess.SecondFactor
				}
			}
		}

		if isCookieSession {
			state.mu.Lock()
			matched := false
			ownerTOTPEnabled := false
			for _, user := range state.users {
				if user.Username == username {
					role = user.Role
					matched = true
					ownerTOTPEnabled = user.TOTPEnabled
					break
				}
			}
			// A fallback-minted bootstrap session has no user row by design;
			// it skips user-match revocation ONLY while the NaivePassword
			// fallback precondition still holds — zero users, fallback
			// configured, and the instance never provisioned (#1112). The
			// moment a real account exists or the instance was ever
			// provisioned, it is revoked like any orphaned session.
			bootstrapValid := sessionBootstrap && username == "admin" &&
				len(state.users) == 0 && state.settings.NaivePassword != "" &&
				!state.usersProvisionedLocked()
			state.mu.Unlock()
			// Second factor, fail closed: once the owner has TOTP enabled, a
			// session without the second-factor mark — e.g. one minted before
			// enrollment — is revoked rather than trusted. The pending_2fa
			// cookie is never consulted here; only a verified session counts
			// (#1172).
			if (matched && ownerTOTPEnabled && !sessionSecondFactor) || (!matched && !bootstrapValid) {
				state.sessionRegistry().Delete(sessionToken)
				username, role, isCookieSession = "", "", false
			}
		}

		if username == "" {
			state.mu.Lock()
			noUsers := len(state.users) == 0
			provisioned := state.usersProvisionedLocked()
			state.mu.Unlock()
			// Dev-anonymous admin exists only for a never-configured
			// instance. Once users have EVER existed, a zero-user state —
			// e.g. a rollback to a pre-setup revision — fails closed and the
			// operator recovers through `veil admin reset`/`veil admin set`
			// (#1100).
			if opts.AllowDevAnonymous && noUsers && !provisioned && opts.Token == "" {
				username = "dev-anonymous"
				role = "admin"
			}
		}

		if username == "" {
			state.mu.Lock()
			lockdown := state.isProvisionedAuthLockdownLocked()
			state.mu.Unlock()
			w.Header().Set("WWW-Authenticate", `Bearer realm="Veil API"`)
			if lockdown {
				writeError(w, "missing or invalid API token or session; "+provisionedRecoveryHint, http.StatusUnauthorized)
			} else {
				writeError(w, "missing or invalid API token or session", http.StatusUnauthorized)
			}
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

func isHealthProbePath(path string) bool {
	switch path {
	case "/healthz", "/livez", "/readyz":
		return true
	default:
		return false
	}
}

func csrfExemptPublicMutation(path string) bool {
	switch path {
	// The pending_2fa verify endpoint sits in the same position as login: the
	// SPA completing a second factor may hold a stale veil_session whose CSRF
	// token it never had, and the pending cookie — SameSite=Lax, password-gated
	// — is itself the proof the flow is on-site (#1172).
	case "/api/auth/login", "/api/setup/complete", "/api/v1/auth/totp/verify":
		return true
	default:
		return false
	}
}

func isMutatingRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// There is deliberately no "read-only POST" helper: POST endpoints that serve
// viewers (apply plan, tool diagnostics, RU preview) are classified by
// endpointPolicies like everything else. The old isReadOnlyDiagnosticRequest
// allowlist was never consulted by the middleware and wrongly grouped
// /api/client-links/qr — which emits admin secret material — with diagnostics.
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
