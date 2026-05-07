package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// ServiceActionRequest is the body for service control endpoints.
type ServiceActionRequest struct {
	Confirm bool `json:"confirm"`
}

// ServiceActionResponse is the result of a service control operation.
type ServiceActionResponse struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

var allowedServices = map[string]bool{
	"veil":      true,
	"caddy":     true,
	"hysteria2": true,
	"sing-box":  true,
	"mieru":     true,
}

var serviceControlRunner = runServiceControl

func handleServiceAction(w http.ResponseWriter, r *http.Request, name, action string) {
	if !allowedServices[name] {
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
	resp := ServiceActionResponse{Service: name, Action: action}
	cmd := exec.Command("systemctl", action, name+".service")
	out, err := cmd.CombinedOutput()
	resp.Output = strings.TrimSpace(string(out))
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Success = true
	}
	return resp
}

var _ = json.Marshal // keep json import
var _ = fmt.Sprintf  // keep fmt import
