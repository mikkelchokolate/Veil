package api

import "net/http"

func (s *managementState) handleReliableUserItemRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if !exactUserNamePath(r.URL.Path) {
			writeNotFound(w)
			return
		}
		s.handleAtomicUserDelete(w, r)
		return
	}
	s.handleUsersRouteWithAdminInvariant(w, r)
}
