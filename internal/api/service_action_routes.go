package api

import (
	"net/http"
	"strings"
)

type ServiceActionRoutes struct{}

func (ServiceActionRoutes) Paths() []string {
	return []string{"/api/services/"}
}

func (ServiceActionRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/services/", handleServiceActionRoute)
}

func handleServiceActionRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	// Parse /api/services/{name}/restart
	path := strings.TrimPrefix(r.URL.Path, "/api/services/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		writeError(w, "invalid path, expected /api/services/{name}/restart", http.StatusBadRequest)
		return
	}
	name, action := parts[0], parts[1]
	if action != "restart" {
		writeError(w, "unsupported action: "+action, http.StatusBadRequest)
		return
	}
	handleServiceAction(w, r, name, action)
}
