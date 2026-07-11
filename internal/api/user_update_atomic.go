package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/panel"
	"golang.org/x/crypto/bcrypt"
)

// handleAtomicUserUpdate keeps password preservation and role/locale changes in
// the same management-state critical section. A role-only request must preserve
// whichever password hash is current when the mutation is applied, rather than
// writing a stale hash captured before another concurrent update.
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
		hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
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
		found := false
		for _, user := range mutation.Users() {
			if user.Username == username {
				found = true
				break
			}
		}
		if !found {
			return errUserNotFound
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

	writeJSON(w, map[string]any{
		"username": updated.Username,
		"role":     updated.Role,
		"locale":   panel.NormalizeLocale(updated.Locale),
	})
	_, _ = s.sessionRegistry().DeleteByUsername(username)
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
