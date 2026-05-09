package api

import (
	"encoding/json"
	"net/http"

	"github.com/veil-panel/veil/internal/service"
)

type StatusResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Mode          string          `json:"mode"`
	Services      []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	Managed     bool   `json:"managed"`
	Transport   string `json:"transport,omitempty"`
	Unit        string `json:"unit,omitempty"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
}

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
		_ = json.NewEncoder(w).Encode(NewStatusResponseBuilder(routes.Info, buildServiceStatuses).Build())
	}
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	return service.ReadSystemdServiceStatus(unit)
}
