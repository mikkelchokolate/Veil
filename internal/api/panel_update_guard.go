package api

import "net/http"

func (s *managementState) beginPanelUpdate(w http.ResponseWriter) bool {
	if !s.updateMu.TryLock() {
		writeError(w, "another panel update is already in progress", http.StatusConflict)
		return false
	}
	if !s.beginServiceAction(w) {
		s.updateMu.Unlock()
		return false
	}
	return true
}

func (s *managementState) endPanelUpdate() {
	s.serviceActionMu.Unlock()
	s.updateMu.Unlock()
}
