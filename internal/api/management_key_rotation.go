package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func (s *managementState) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	if err := validateEmptyJSONBody(r); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.privileged == nil {
		writePrivilegedError(w, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.privileged.RotateKey(r.Context(), privileged.RotateKeyRequest{}); err != nil {
		reloadErr := NewManagementStateLifecycle(s).ReloadLocked()
		if reloadErr != nil {
			s.startupStateLoadFailed = true
			s.allowDevAnonymous = false
			s.recordRequestAudit(r, audit.Record{
				Action: "security.key.rotate", Target: "state", Success: false,
				Error: err.Error() + "; recovery failed: " + reloadErr.Error(),
			})
			writeError(w, "state key rotation failed and recovery could not establish a coherent key/state pair", http.StatusInternalServerError)
			return
		}
		s.recordRequestAudit(r, audit.Record{
			Action: "security.key.rotate", Target: "state", Success: false, Error: err.Error(),
		})
		writePrivilegedError(w, err)
		return
	}
	if err := NewManagementStateLifecycle(s).ReloadLocked(); err != nil {
		s.startupStateLoadFailed = true
		s.allowDevAnonymous = false
		s.recordRequestAudit(r, audit.Record{
			Action: "security.key.rotate", Target: "state", Success: false, Error: err.Error(),
		})
		writeError(w, "state key rotated but Panel reload failed", http.StatusInternalServerError)
		return
	}
	s.startupStateLoadFailed = false
	revoked, err := s.sessionRegistry().DeleteAllExceptPersisted(currentSessionToken(r))
	if err != nil {
		writeError(w, "state key rotated but sessions could not be revoked", http.StatusInternalServerError)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Action: "security.key.rotate", Target: "state", Success: true,
		Details: map[string]any{"revokedSessions": revoked},
	})
	writeJSON(w, map[string]any{"success": true, "revokedSessions": revoked})
}
