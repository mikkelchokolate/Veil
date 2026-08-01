package api

import (
	"context"
	"net/http"
	"strings"

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
	if !s.applyTrackingEnabled() {
		if s.requireApplyTracking {
			writeError(w, "durable apply tracking is required for service mutations", http.StatusServiceUnavailable)
			return
		}
		// Unpersisted development/test states do not represent a production
		// runtime. Preserve their local adapter behavior without weakening the
		// persisted production path above.
		err := s.privileged.ServiceAction(r.Context(), privileged.ServiceActionRequest{
			Unit: runtime.Unit, Action: privileged.ServiceAction(action),
		})
		resp.Success = err == nil
		if err != nil {
			resp.Error = err.Error()
			writePrivilegedError(w, err)
			return
		}
		writeJSON(w, resp)
		return
	}
	revision, err := s.ensureRunnableRevision()
	if err != nil {
		writeError(w, "service operation fence unavailable", http.StatusServiceUnavailable)
		return
	}
	_, err = s.applyRunner.RunOperationContext(r.Context(), revision, "service:"+action, actorFromRequest(r),
		veilapply.ContextExecutorFunc(func(ctx context.Context, pinnedRevision uint64) (veilapply.Result, error) {
			result, err := s.convergeRevisionForSideEffect(ctx, pinnedRevision)
			if err != nil {
				return result, err
			}
			fence, ok := veilapply.FenceFromContext(ctx)
			if !ok || fence.Owner == "" || fence.Generation == 0 {
				return veilapply.Result{Success: false, ErrorCode: "FENCE_REQUIRED"}, veilapply.ErrApplyLeaseLost
			}
			if err := veilapply.MarkRuntimeMutationStarting(ctx, veilapply.PublicationDetails{
				Artifacts: []string{runtime.Unit}, ServicePhase: action,
			}); err != nil {
				return veilapply.Result{Success: false, ErrorCode: "PUBLICATION_INTENT"}, err
			}
			actionErr := s.privileged.ServiceAction(ctx, privileged.ServiceActionRequest{
				Unit: runtime.Unit, Action: privileged.ServiceAction(action),
				Fence: privileged.FenceToken{Owner: fence.Owner, Generation: fence.Generation,
					LeaseExpiresAt: fence.LeaseExpiresAt, OperationID: fence.OperationID},
			})
			operation := veilapply.OperationResult{Type: "service", Target: runtime.Unit, Success: actionErr == nil}
			if actionErr != nil {
				operation.Detail = actionErr.Error()
			}
			result.Success = actionErr == nil
			result.Operations = append(result.Operations, operation)
			return result, actionErr
		}))
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
