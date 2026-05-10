package api

import (
	"encoding/json"
	"net/http"

	"github.com/veil-panel/veil/internal/service"
)

type StatusResponse = service.StatusResponse
type ServiceStatus = service.ServiceStatus
type ServiceRuntimeStatus = service.RuntimeStatus

var serviceStatusReader = readSystemdServiceStatus

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
		info := service.StatusInfo{Version: routes.Info.Version, Mode: routes.Info.Mode}
		_ = json.NewEncoder(w).Encode(service.NewStatusResponseBuilder(info, buildServiceStatuses).Build())
	}
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	return service.ReadSystemdServiceStatus(unit)
}
