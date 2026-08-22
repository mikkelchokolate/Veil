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
		currentRole := ""
		adminCount := 0
		for _, user := range users {
			if user.Role == "admin" {
				adminCount++
			}
			if user.Username == username {
				found = true
				currentRole = user.Role
			}
		}
		if !found {
			return errUserNotFound
		}
		if currentRole == "admin" && req.Role != "admin" && adminCount <= 1 {
			return managementstate.ErrLastAdministrator
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernamePersisted(username); revokeErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		var updateErr error
		updated, updateErr = mutation.UpdateUser(username, update)
		return updateErr
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
