package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/service"
)

// ServiceActionRequest is the body for service control endpoints.
type ServiceActionRequest struct {
	Confirm bool `json:"confirm"`
}

// ServiceActionResponse is the result of a service control operation.
type ServiceActionResponse = service.ManualActionResponse

var serviceControlRunner = runServiceControl

func handleServiceActionRoute(w http.ResponseWriter, r *http.Request) {
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
	handleServiceAction(w, r, name, action)
}

func handleServiceAction(w http.ResponseWriter, r *http.Request, name, action string) {
	if !service.NewManualServiceControl(NewManagedRuntimeCatalog(), nil).Allows(name) {
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

	resp := serviceControlRunner(name, action)
	status := http.StatusOK
	if !resp.Success {
		status = http.StatusInternalServerError
	}
	writeJSONStatus(w, status, resp)
}

func runServiceControl(name, action string) ServiceActionResponse {
	return service.NewManualServiceControl(NewManagedRuntimeCatalog(), nil).Run(name, action)
}
