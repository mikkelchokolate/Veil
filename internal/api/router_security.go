package api

import (
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
