package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/panel"
)

// handleAtomicUserUpdate keeps password preservation, role/locale changes, and
// session revocation in the same management-state critical section. A role-only
// request must preserve whichever password hash is current when the mutation is
// applied, and no login may create a session between the update and revocation.
func (s *managementState) handleAtomicUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}

	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	var req struct {
		Password string `json:"password,omitempty"`
		Role     string `json:"role"`
		Locale   string `json:"locale,omitempty"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if req.Role != "admin" && req.Role != "viewer" {
		writeError(w, "valid role (admin/viewer) is required", http.StatusBadRequest)
		return
	}
	if req.Locale != "" {
		locale, ok := panel.ParseLocale(req.Locale)
		if !ok {
			writeError(w, "locale must be en or ru", http.StatusBadRequest)
			return
		}
		req.Locale = locale
	}

	passwordHash := ""
	if req.Password != "" {
		hashed, err := s.hashPassword([]byte(req.Password))
		if err != nil {
			writeError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		passwordHash = string(hashed)
	}

	update := model.User{
		Username:     username,
		PasswordHash: passwordHash,
		Role:         req.Role,
		Locale:       req.Locale,
	}
	var updated model.User
	err := s.withMutation(func(mutation managementstate.Mutation) error {
		users := mutation.Users()
		found := false
		var prior model.User
		adminCount := 0
		for _, user := range users {
			if user.Role == "admin" {
				adminCount++
			}
			if user.Username == username {
				found = true
				prior = user
			}
		}
		if !found {
			return errUserNotFound
		}
		if prior.Role == "admin" && req.Role != "admin" && adminCount <= 1 {
			return managementstate.ErrLastAdministrator
		}
		// Persist the user mutation BEFORE revoking sessions: when the state
		// save fails the mutation rolls back and the user's sessions must
		// remain valid rather than being revoked for a change that never
		// committed.
		//
		// Journal the revocation intent BEFORE the mutation commits: a crash
		// between the commit and the delete_many record would otherwise leave
		// sessions minted under the old credential valid past restart (#1059).
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		var updateErr error
		updated, updateErr = mutation.UpdateUser(username, update)
		if updateErr != nil {
			// The mutation rolled back, so retract the intent: the user's
			// sessions must stay valid for a change that never committed.
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return updateErr
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernamePersisted(username); revokeErr != nil {
			// The user update already committed; restore the previous record
			// so a failed revocation never leaves an updated user whose
			// sessions were meant to be revoked but are still live.
			if _, restoreErr := mutation.UpdateUser(username, prior); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			// The restore committed, so the revocation intent is retracted;
			// when the cancel cannot be journaled the stale intent is a safe
			// re-login at worst.
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		return nil
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Action:  "user.update",
			Target:  username,
			Success: false,
			Error:   err.Error(),
		})
		if errors.Is(err, errSessionRevocationPersistence) {
			writeError(w, errSessionRevocationPersistence.Error(), http.StatusInternalServerError)
			return
		}
		switch err {
		case errUserNotFound:
			writeNotFound(w)
		case managementstate.ErrLastAdministrator:
			writeError(w, managementstate.ErrLastAdministrator.Error(), http.StatusBadRequest)
		default:
			writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()

	writeJSON(w, map[string]any{
		"username": updated.Username,
		"role":     updated.Role,
		"locale":   panel.NormalizeLocale(updated.Locale),
	})
	s.recordRequestAudit(r, audit.Record{
		Action:  "user.update",
		Target:  username,
		Success: true,
		Details: map[string]any{
			"role":            updated.Role,
			"passwordChanged": req.Password != "",
		},
	})
}
