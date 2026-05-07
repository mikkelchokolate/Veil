package api

import (
	"encoding/json"
	"net/http"
)

type StatusRoutes struct {
	Info ServerInfo
}

func (StatusRoutes) Paths() []string {
	return []string{"/api/status"}
}

func (routes StatusRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", routes.handleStatus)
}

func (routes StatusRoutes) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(StatusResponse{
			SchemaVersion: "v1",
			Name:          "Veil",
			Version:       routes.Info.Version,
			Mode:          routes.Info.Mode,
			Services:      buildServiceStatuses(),
		})
	}
}
