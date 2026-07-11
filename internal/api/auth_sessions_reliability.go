package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

func (s *managementState) handlePersistentAuthSessions(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.sessionRegistry().List(currentSessionToken(r)))
	case http.MethodDelete:
		var req struct {
			ID string `json:"id"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			writeError(w, "session id is required", http.StatusBadRequest)
			return
		}
		deleted, err := s.sessionRegistry().DeleteByIDPersisted(req.ID)
		if err != nil {
			s.recordRequestAudit(r, audit.Record{
				Action:  "auth.session.revoke",
				Target:  req.ID,
				Success: false,
				Error:   err.Error(),
			})
			writeError(w, "failed to persist session revocation", http.StatusInternalServerError)
			return
		}
		if !deleted {
			s.recordRequestAudit(r, audit.Record{
				Action:  "auth.session.revoke",
				Target:  req.ID,
				Success: false,
				Error:   "session not found",
			})
			writeNotFound(w)
			return
		}
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.session.revoke",
			Target:  req.ID,
			Success: true,
		})
		writeJSON(w, map[string]any{"success": true})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}
