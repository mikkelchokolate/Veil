package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type StatusResponse = service.StatusResponse
type ServiceStatus = service.ServiceStatus
type ServiceRuntimeStatus = service.RuntimeStatus

var serviceStatusReader = defaultReadSystemdServiceStatus

type StatusRoutes struct {
	Info  ServerInfo
	State *managementState
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
		catalog := NewManagedRuntimeCatalog()
		runtimes := catalog.Runtimes()
		units := make([]string, 0, len(runtimes))
		for _, runtime := range runtimes {
			units = append(units, runtime.Unit)
		}
		if routes.State == nil || routes.State.privileged == nil {
			writePrivilegedError(w, &privileged.Error{
				Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
			})
			return
		}
		result, err := routes.State.privileged.ServiceStatus(r.Context(), privileged.ServiceStatusRequest{Units: units})
		if err != nil {
			writePrivilegedError(w, err)
			return
		}
		byUnit := make(map[string]privileged.ServiceStatus, len(result.Services))
		for _, status := range result.Services {
			byUnit[status.Unit] = status
		}
		statuses := make([]ServiceStatus, 0, len(runtimes))
		for _, runtime := range runtimes {
			status := byUnit[runtime.Unit]
			statuses = append(statuses, ServiceStatus{
				Name: runtime.Name, Managed: true, Transport: runtime.Transport, Unit: runtime.Unit,
				LoadState: status.LoadState, ActiveState: status.ActiveState, SubState: status.SubState, Error: status.Error,
			})
		}
		writeJSON(w, service.NewStatusResponseBuilder(info, func() []ServiceStatus { return statuses }).Build())
	}
}

func defaultReadSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	return service.ReadSystemdServiceStatus(unit)
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	return serviceStatusReader(unit)
}
