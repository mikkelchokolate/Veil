package api

import (
	"context"
	"log"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

var firewallStatusReader = func() (bool, error) {
	return firewall.NewStatusReader(nil).Active()
}

func (s *managementState) handleClientLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Attach normalized Client+Binding credentials so admin links include
	// normalized clients, matching what the live config actually renders
	// (audit #65/#68: links and server config diverged).
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response, err := protocols.BuildClientLinks(s.settings, inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientaccess.NewClientLinkDeliveryHeaders().Apply(w.Header())
	writeJSON(w, response)
}

func (s *managementState) handleClientLinksSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := r.URL.Query()
	if err := clientaccess.ValidateClientSubscriptionQuery(query); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	format := query.Get("format")
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := protocols.BuildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscription, err := clientaccess.BuildClientSubscription(response, format)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientaccess.NewClientSubscriptionDeliveryHeaders(subscription).Apply(w.Header())
	_, _ = w.Write([]byte(subscription.Body))
}

func (s *managementState) handleFirewall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active, err := firewallStatusReader()
	if err != nil {
		writeError(w, "firewall status unavailable", http.StatusServiceUnavailable)
		return
	}
	rules := firewall.BuildRuleResponses(s.settings, s.inbounds)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{
			"active": active,
			"rules":  rules,
		})
	} else {
		setJSONHeaders(w)
	}
}

func (s *managementState) handleApplyPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := NewManagementApplyContextWithContext(s, r.Context()).buildApplyPlanLocked()
	status := http.StatusOK
	if !plan.Valid {
		status = http.StatusBadRequest
		if len(plan.Issues) > 0 {
			status = http.StatusUnprocessableEntity
		}
	}
	writeJSONStatus(w, status, plan)
}

func (s *managementState) handleApplyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history, err := s.applyHistoryLocked().Query(r.URL.Query())
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, history)
}

func (s *managementState) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req ApplyRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeError(w, "confirm=true is required to write staged apply files", http.StatusBadRequest)
		return
	}
	if applyRequiresServiceActionLock(req) {
		if !s.beginServiceAction(w) {
			return
		}
		defer s.serviceActionMu.Unlock()
	}
	if s.applyTrackingEnabled() {
		desiredRevision, err := s.ensureRunnableRevision()
		if err != nil {
			log.Printf("failed to read desired revision: %v", err)
			writeError(w, "failed to read desired revision", http.StatusServiceUnavailable)
			return
		}
		var response ApplyResponse
		status := http.StatusInternalServerError
		var workflowErr error
		_, runErr := s.applyRunner.RunOperationContext(r.Context(), desiredRevision, "manual", actorFromRequest(r),
			apply.ContextExecutorFunc(func(ctx context.Context, revision uint64) (apply.Result, error) {
				var result apply.Result
				response, status, result, workflowErr = s.executeApplyRevisionRequestContext(ctx, revision, req)
				return result, workflowErr
			}))
		if status == http.StatusBadRequest && len(response.Plan.Issues) > 0 {
			status = http.StatusUnprocessableEntity
		}
		s.logUserAction(r, "apply_configuration", "system", runErr == nil && status == http.StatusOK, "")
		if runErr != nil {
			log.Printf("apply runner rejected revision %d: %v", desiredRevision, runErr)
		}
		if workflowErr != nil {
			log.Printf("apply workflow failed for revision %d: %v", desiredRevision, workflowErr)
			writeError(w, workflowErr.Error(), status)
			return
		}
		if status != http.StatusOK {
			if status == http.StatusInternalServerError && runErr != nil {
				writeError(w, runErr.Error(), http.StatusServiceUnavailable)
				return
			}
			writeJSONStatus(w, status, response)
			return
		}
		if runErr != nil {
			writeError(w, runErr.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, response)
		return
	}
	s.mu.Lock()
	response, status, err := NewApplyWorkflow(NewManagementApplyContextWithContext(s, r.Context())).RunLocked(req)
	s.mu.Unlock()
	if status == http.StatusBadRequest && len(response.Plan.Issues) > 0 {
		status = http.StatusUnprocessableEntity
	}
	s.logUserAction(r, "apply_configuration", "system", err == nil && status == http.StatusOK, "")
	if err != nil {
		writeError(w, err.Error(), status)
		return
	}
	if status != http.StatusOK {
		writeJSONStatus(w, status, response)
		return
	}
	writeJSON(w, response)
}

// autoApplyLocked runs a full live + services apply while already holding s.mu.
// It best-effort acquires the service action lock via TryLock so it cannot
// deadlock with concurrent explicit apply requests. Returns the apply response
// and whether it succeeded. Caller must hold s.mu.
func (s *managementState) autoApplyLocked(r *http.Request) (ApplyResponse, bool) {
	if !autoApplyAfterMutation {
		return ApplyResponse{}, false
	}
	if !s.serviceActionMu.TryLock() {
		s.logUserAction(r, "auto_apply_configuration", "system", false, "service action lock busy")
		return ApplyResponse{}, false
	}
	defer s.serviceActionMu.Unlock()
	operationContext := s.lifecycleContext()
	if r != nil {
		operationContext = r.Context()
	}
	response, status, err := NewApplyWorkflow(NewManagementApplyContextWithContext(s, operationContext)).RunLocked(ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	success := err == nil && status == http.StatusOK
	details := ""
	if err != nil {
		details = err.Error()
	} else if status != http.StatusOK {
		details = http.StatusText(status)
	}
	s.logUserAction(r, "auto_apply_configuration", "system", success, details)
	return response, success
}

// autoApplyResultLocked is the rich variant of autoApplyLocked that records a
// durable apply job and returns revision + job information for the HTTP
// response. When revision tracking is disabled (no StatePath) it falls back to
// the legacy synchronous auto-apply and reports only success. Caller holds s.mu.
func (s *managementState) autoApplyResultLocked(r *http.Request, actor string) autoApplyOutcome {
	outcome := autoApplyOutcome{}
	if !autoApplyAfterMutation {
		return outcome
	}
	if !s.applyTrackingEnabled() {
		_, ok := s.autoApplyLocked(r)
		outcome.legacy = true
		outcome.success = ok
		return outcome
	}
	revisions := s.applyRevisions
	runner := s.applyRunner
	rev, err := revisions.Get()
	if err != nil {
		s.logUserAction(r, "auto_apply_configuration", "system", false, "read revisions: "+err.Error())
		outcome.success = false
		return outcome
	}
	outcome.revision = rev
	var job apply.Job
	var runErr error
	operationContext := s.lifecycleContext()
	if r != nil {
		operationContext = r.Context()
	}
	func() {
		s.mu.Unlock()
		defer s.mu.Lock()
		job, runErr = runner.RunLatest(operationContext, "mutation", actor)
	}()
	if job.ID != "" {
		outcome.job = &job
	}
	outcome.success = runErr == nil && (job.ID == "" || job.Status == apply.StatusSucceeded)
	s.registerTrafficProvidersLocked()
	if after, err := revisions.Get(); err == nil {
		outcome.revision = after
	}
	details := ""
	if !outcome.success {
		details = job.ErrorMessage
	}
	s.logUserAction(r, "auto_apply_configuration", "system", outcome.success, details)
	return outcome
}

// autoApplyOutcome carries the apply result surfaced to the HTTP client.
type autoApplyOutcome struct {
	revision apply.Revisions
	job      *apply.Job
	success  bool
	legacy   bool // true when the legacy (untracked) synchronous path was used
}

// applyStateView derives the public system state (synced/pending/applying/...)
// from revisions and the latest job.
func (s *managementState) applyStateViewLocked() applyStateResponse {
	resp := applyStateResponse{State: apply.StateSynced}
	if !s.applyTrackingEnabled() {
		return resp
	}
	rev, err := s.applyRevisions.Get()
	if err != nil {
		resp.State = apply.StateDegraded
		resp.LastError = &applyErrorView{Code: "database_unavailable", Message: "apply state is unavailable"}
		return resp
	}
	resp.DesiredRevision = rev.Desired
	resp.AppliedRevision = rev.Applied
	resp.State = deriveSystemState(rev, nil)
	jobs, err := s.applyJobs.List(1)
	if err != nil {
		resp.State = apply.StateDegraded
		resp.LastError = &applyErrorView{Code: "database_unavailable", Message: "apply jobs are unavailable"}
		return resp
	}
	if len(jobs) > 0 {
		latest := jobs[0]
		resp.State = deriveSystemState(rev, &latest)
		if latest.Active() {
			resp.ActiveJobID = latest.ID
		}
	}
	if lastOK, ok, _ := s.applyJobs.LatestWithStatus(apply.StatusSucceeded); ok {
		resp.LastSuccessfulJobID = lastOK.ID
	}
	if lastFail, ok, _ := s.applyJobs.LatestFailed(); ok {
		resp.LastFailedJobID = lastFail.ID
	}
	if len(jobs) > 0 {
		latest := jobs[0]
		if resp.State == apply.StateFailed || resp.State == apply.StateRolledBack {
			if latest.Status == apply.StatusFailed || latest.Status == apply.StatusRolledBack || latest.Status == apply.StatusRollbackFailed {
				resp.LastError = &applyErrorView{Code: latest.ErrorCode, Message: latest.ErrorMessage}
			}
		}
	}
	return resp
}

// deriveSystemState maps revisions + latest job to the public system state.
func deriveSystemState(rev apply.Revisions, latest *apply.Job) string {
	if latest != nil {
		switch latest.Status {
		case apply.StatusPending, apply.StatusPlanning, apply.StatusValidating:
			return apply.StatePending
		case apply.StatusApplying, apply.StatusHealthCheck:
			return apply.StateApplying
		case apply.StatusRollingBack:
			return apply.StateRollingBack
		case apply.StatusRolledBack:
			return apply.StateRolledBack
		case apply.StatusFailed, apply.StatusRollbackFailed:
			if rev.Desired > rev.Applied {
				return apply.StateFailed
			}
		}
	}
	if rev.Desired > rev.Applied {
		return apply.StatePending
	}
	return apply.StateSynced
}

// applyStateResponse is the JSON shape returned by GET /api/apply/state.
type applyStateResponse struct {
	DesiredRevision     uint64          `json:"desiredRevision"`
	AppliedRevision     uint64          `json:"appliedRevision"`
	State               string          `json:"state"`
	ActiveJobID         string          `json:"activeJobId,omitempty"`
	LastSuccessfulJobID string          `json:"lastSuccessfulJobId,omitempty"`
	LastFailedJobID     string          `json:"lastFailedJobId,omitempty"`
	LastError           *applyErrorView `json:"lastError,omitempty"`
}

type applyErrorView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
