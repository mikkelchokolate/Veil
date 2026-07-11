package api

import "net/http"

func (s *managementState) beginServiceAction(w http.ResponseWriter) bool {
	if s.serviceActionMu.TryLock() {
		return true
	}
	writeError(w, "another service action is already in progress", http.StatusConflict)
	return false
}
