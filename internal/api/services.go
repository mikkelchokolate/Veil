package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/veil-panel/veil/internal/service"
)

// ServiceActionRequest is the body for service control endpoints.
type ServiceActionRequest struct {
	Confirm bool `json:"confirm"`
}

// ServiceActionResponse is the result of a service control operation.
type ServiceActionResponse = service.ManualActionResponse

var serviceControlRunner = runServiceControl

func handleServiceAction(w http.ResponseWriter, r *http.Request, name, action string) {
	if !NewServiceControlCommand().Allows(name) {
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

var _ = json.Marshal // keep json import
var _ = fmt.Sprintf  // keep fmt import
