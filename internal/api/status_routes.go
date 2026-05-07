package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
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

func buildServiceStatuses() []ServiceStatus {
	services := []ServiceStatus{
		{Name: "veil", Managed: true, Unit: "veil.service"},
		{Name: "naive", Managed: true, Transport: "tcp", Unit: "caddy.service"},
		{Name: "hysteria2", Managed: true, Transport: "udp", Unit: "hysteria2.service"},
		{Name: "sing-box", Managed: true, Unit: "sing-box.service"},
	}
	for i := range services {
		runtime := serviceStatusReader(services[i].Unit)
		services[i].LoadState = runtime.LoadState
		services[i].ActiveState = runtime.ActiveState
		services[i].SubState = runtime.SubState
		services[i].Error = runtime.Error
	}
	return services
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	status := ServiceRuntimeStatus{Unit: unit, LoadState: "unknown", ActiveState: "unknown", SubState: "unknown"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx,
		"systemctl",
		"show",
		unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--no-page",
	).CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			if value != "" {
				status.LoadState = value
			}
		case "ActiveState":
			if value != "" {
				status.ActiveState = value
			}
		case "SubState":
			if value != "" {
				status.SubState = value
			}
		}
	}
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}
