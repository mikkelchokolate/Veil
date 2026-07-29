package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
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
	if !s.beginServiceAction(w) {
		return
	}
	defer s.serviceActionMu.Unlock()

	resp := ServiceActionResponse{Service: name, Action: action}
	if s.privileged == nil {
		writePrivilegedError(w, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		})
		return
	}
	var lease veilapply.Lease
	if s.db != nil {
		owner := fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
		acquiredLease, acquired, leaseErr := veilapply.NewLeaseStore(s.db).Acquire(owner, "service:"+name, time.Now().UTC(), 30*time.Second)
		if leaseErr != nil {
			writeError(w, "service operation fence unavailable", http.StatusServiceUnavailable)
			return
		}
		if !acquired {
			writeError(w, "another runtime mutation is in progress", http.StatusLocked)
			return
		}
		lease = acquiredLease
	}
	err := s.privileged.ServiceAction(r.Context(), privileged.ServiceActionRequest{
		Unit: runtime.Unit, Action: privileged.ServiceAction(action),
		Fence: privileged.FenceToken{Owner: lease.Owner, Generation: lease.Generation},
	})
	if lease.Generation > 0 {
		err = errors.Join(err, veilapply.NewLeaseStore(s.db).Release(lease.Owner, lease.Generation))
	}
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
