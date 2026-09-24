package api

import (
	"context"
	"net/http"
	"time"

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

	fence, releaseFence, fenceErr := s.acquireRuntimeFence("security-key-rotate")
	if fenceErr != nil {
		writeError(w, "state key rotation fencing lease is unavailable: "+fenceErr.Error(), http.StatusConflict)
		return
	}

	if err := s.privileged.RotateKey(r.Context(), privileged.RotateKeyRequest{Fence: fence}); err != nil {
		lifecycle := NewManagementStateLifecycle(s)
		recoveryCtx, cancelRecovery := context.WithTimeout(context.Background(), 30*time.Second)
		// The same fencing lease covers the recovery mutation; a second
		// acquisition would deadlock against the lease we still hold.
		recoveryErr := lifecycle.recoverPendingKeyRotationWithFence(recoveryCtx, fence)
		cancelRecovery()
		var reloadErr error
		if recoveryErr != nil {
			reloadErr = recoveryErr
		} else {
			reloadErr = lifecycle.ReloadLocked()
		}
		// The fenced mutation section ends here; the lease must not leak into
		// later auto-apply runs that acquire it themselves.
		releaseFence()
		if reloadErr != nil {
			s.startupStateLoadFailed = true
			s.startupStateLoadErr = reloadErr
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
		releaseFence()
		s.startupStateLoadFailed = true
		s.startupStateLoadErr = err
		s.allowDevAnonymous = false
		s.recordRequestAudit(r, audit.Record{
			Action: "security.key.rotate", Target: "state", Success: false, Error: err.Error(),
		})
		writeError(w, "state key rotated but Panel reload failed", http.StatusInternalServerError)
		return
	}
	// Release before session revocation/auto-apply: the durable lease is
	// singleton, and a later apply run must be able to claim it.
	releaseFence()
	s.startupStateLoadFailed = false
	s.startupStateLoadErr = nil
	revoked, err := s.sessionRegistry().DeleteAllExceptPersisted(currentSessionToken(r))
	if err != nil {
		writeError(w, "state key rotated but sessions could not be revoked", http.StatusInternalServerError)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Action: "security.key.rotate", Target: "state", Success: true,
		Details: map[string]any{"revokedSessions": revoked},
	})
	// Rotation itself already advanced desired (re-encrypt + snapshot).
	// Without the mutation apply envelope the panel stays Pending until a
	// manual reconcile, the same drift operators hit after other writes.
	actor, _ := r.Context().Value(contextKeyUsername).(string)
	outcome := s.autoApplyResultLocked(r, actor)
	payload := map[string]any{"revokedSessions": revoked}
	s.mergeOutcomeInto(payload, outcome)
	writeJSON(w, payload)
}
