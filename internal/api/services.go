package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// ServiceActionRequest is the body for service control endpoints.
type ServiceActionRequest struct {
	Confirm bool `json:"confirm"`
}

// ServiceActionResponse is the result of a service control operation.
type ServiceActionResponse = service.ManualActionResponse

func (s *managementState) handleServiceActionRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
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
	s.handleServiceAction(w, r, name, action)
}

func (s *managementState) handleServiceAction(w http.ResponseWriter, r *http.Request, name, action string) {
	runtime, ok := managedRuntimeByActionName(s, name)
	if !ok || !runtime.ManualRestart {
		writeError(w, "unknown service: "+name, http.StatusBadRequest)
		return
	}

	var req ServiceActionRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeError(w, "confirm=true is required", http.StatusBadRequest)
		return
	}

	resp := ServiceActionResponse{Service: name, Action: action}
	if s.privileged == nil {
		writePrivilegedError(w, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		})
		return
	}
	err := s.privileged.ServiceAction(r.Context(), privileged.ServiceActionRequest{
		Unit: runtime.Unit, Action: privileged.ServiceAction(action),
	})
	resp.Success = err == nil
	if err != nil {
		resp.Error = err.Error()
		s.logUserAction(r, "service_"+action, name, false, err.Error())
		writePrivilegedError(w, err)
		return
	}
	s.logUserAction(r, "service_"+action, name, true, "")
	writeJSON(w, resp)
}
