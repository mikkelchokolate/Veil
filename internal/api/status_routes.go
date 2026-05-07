package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
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

type ServiceRuntimeStatus struct {
	Unit        string
	LoadState   string
	ActiveState string
	SubState    string
	Error       string
}

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
		_ = json.NewEncoder(w).Encode(StatusResponse{
			SchemaVersion: "v1",
			Name:          "Veil",
			Version:       routes.Info.Version,
			Mode:          routes.Info.Mode,
			Services:      buildServiceStatuses(),
		})
	}
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	command := NewSystemdServiceStatusCommand(unit)
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout())
	defer cancel()
	output, err := exec.CommandContext(ctx, command.Name(), command.Args()...).CombinedOutput()
	status := NewSystemdServiceStatusParser().Parse(unit, string(output))
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}
