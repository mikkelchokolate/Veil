package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

func (s *managementState) handleAtomicUserDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}

	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	err := s.withMutation(func(mutation managementstate.Mutation) error {
		users := mutation.Users()
		found := false
		targetRole := ""
		adminCount := 0
		for _, user := range users {
			if user.Role == "admin" {
				adminCount++
			}
			if user.Username == username {
				found = true
				targetRole = user.Role
			}
		}
		if !found {
			return errUserNotFound
		}
		if targetRole == "admin" && adminCount <= 1 {
			return managementstate.ErrLastAdministrator
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernamePersisted(username); revokeErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		return mutation.DeleteUser(username)
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Action:  "user.delete",
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

	s.recordRequestAudit(r, audit.Record{
		Action:  "user.delete",
		Target:  username,
		Success: true,
	})
	w.WriteHeader(http.StatusNoContent)
}
