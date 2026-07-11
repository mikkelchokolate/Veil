package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

// handleLogoutWithSettingsSnapshot reads PanelAccess under the management-state
// mutex before constructing the expired cookie. Settings may be updated by a
// concurrent Panel request, so the logout path must not read the struct directly.
func (s *managementState) handleLogoutWithSettingsSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	cookie, err := r.Cookie("veil_session")
	if err == nil {
		actor, role := "", ""
		if session, ok := s.sessionRegistry().Get(cookie.Value); ok {
			actor, role = session.Username, session.Role
		}
		s.sessionRegistry().Delete(cookie.Value)
		s.recordRequestAudit(r, audit.Record{
			Actor:   actor,
			Role:    role,
			Action:  "auth.logout",
			Target:  "panel",
			Success: true,
		})
	}

	s.mu.Lock()
	panelAccess := s.settings.PanelAccess
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "veil_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || panelAccess == "caddy",
		MaxAge:   -1,
	})

	writeJSON(w, map[string]any{"success": true})
}
