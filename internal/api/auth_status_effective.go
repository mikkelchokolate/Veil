package api

import "net/http"

// handleEffectiveAuthStatus mirrors the authentication precedence used by the
// protected API middleware. A valid static token is an administrator credential
// even when the browser also has a viewer cookie, so the Panel must resolve the
// same effective role before enabling or disabling controls.
func (s *managementState) handleEffectiveAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.authToken != "" && validAuthToken(r, s.authToken) {
		writeJSON(w, map[string]any{
			"authenticated": true,
			"username":      "api-token",
			"role":          "admin",
			"locale":        "en",
			"authMethod":    "static-token",
		})
		return
	}
	if cookie, err := r.Cookie("veil_session"); err == nil {
		if _, ok := s.sessionRegistry().Get(cookie.Value); ok {
			s.handleAuthStatus(w, r)
			return
		}
	}
	s.mu.Lock()
	devAnonymous := s.allowDevAnonymous && s.authToken == "" && len(s.users) == 0
	s.mu.Unlock()
	if devAnonymous {
		writeJSON(w, map[string]any{
			"authenticated": true,
			"username":      "dev-anonymous",
			"role":          "admin",
			"locale":        "en",
			"authMethod":    "dev-anonymous",
		})
		return
	}
	s.handleAuthStatus(w, r)
}
