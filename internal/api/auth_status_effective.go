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
	s.handleAuthStatus(w, r)
}
