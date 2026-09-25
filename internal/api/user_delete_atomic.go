package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
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
		if prior.Role == "admin" && adminCount <= 1 {
			return managementstate.ErrLastAdministrator
		}
		// Persist the user deletion BEFORE revoking sessions: when the state
		// save fails the mutation rolls back and the user's sessions must
		// remain valid rather than being revoked for a delete that never
		// committed.
		//
		// Journal the revocation intent BEFORE the delete commits: a crash
		// between the commit and the delete_many record must not resurrect
		// the deleted user's sessions at next load (#1059).
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		if deleteErr := mutation.DeleteUser(username); deleteErr != nil {
			// The delete rolled back, so retract the intent: the user's
			// sessions must stay valid for a change that never committed.
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return deleteErr
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernamePersisted(username); revokeErr != nil {
			// The delete already committed; restore the user record so a
			// failed revocation never deletes a user whose sessions are
			// still live.
			if _, restoreErr := mutation.CreateUser(prior); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		return nil
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
