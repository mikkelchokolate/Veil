package api

import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// applyNotifierFunc adapts a function to client.ApplyNotifier.
type applyNotifierFunc func(kind, id string)

func (f applyNotifierFunc) NotifyMutation(kind, id string) { f(kind, id) }

// initApplySubsystem opens the normalized SQLite store next to the state file
// and wires durable revisions and apply jobs. It is a no-op when no StatePath
// is configured (in-memory/test servers) — revision/apply tracking then
// degrades gracefully and apply falls back to the legacy synchronous path.
func initApplySubsystem(s *managementState) {
	if s.statePath == "" {
		return
	}
	dbPath := filepath.Join(filepath.Dir(s.statePath), "veil.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		// A broken store must not prevent the panel from serving; log and run
		// without revision tracking rather than refusing to start.
		log.Printf("apply subsystem: open %s: %v (revision tracking disabled)", dbPath, err)
		return
	}
	s.db = db
	s.applyRevisions = apply.NewRevisionStore(s.db)
	s.applyJobs = apply.NewJobStore(s.db)
	s.applyRunner = apply.NewRunner(s.applyRevisions, s.applyJobs, s.executeApplyRevision)
}

// initClientSubsystem wires the normalized client domain (Stage 1) onto the
// same SQLite store. It must run AFTER the secrets cipher is loaded because
// the credential store encrypts at rest with it. No-op when no store/cipher.
func initClientSubsystem(s *managementState) {
	if s.db == nil || s.cipher == nil {
		return
	}
	clientRepo := client.NewRepository(s.db)
	clientCreds := client.NewCredentialStore(s.db, s.cipher)
	s.clientService = client.NewService(clientRepo, clientCreds, applyNotifierFunc(func(kind, id string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.autoApplyResultLocked(nil, "system")
	}))
	s.tokenStore = client.NewTokenStore(s.db)
	s.subRenderer = client.NewSubscriptionRenderer(clientRepo, clientCreds)
}

// applyTrackingEnabled reports whether durable revisions/jobs are available.
func (s *managementState) applyTrackingEnabled() bool {
	return s.applyRunner != nil && s.applyRevisions != nil && s.applyJobs != nil
}

// bumpDesiredRevisionLocked records a committed configuration mutation. Caller
// must hold s.mu. Returns the new desired revision, or 0 when tracking is
// disabled. Errors are logged but never fail the mutation that triggered them.
func (s *managementState) bumpDesiredRevisionLocked() uint64 {
	if !s.applyTrackingEnabled() {
		return 0
	}
	rev, err := s.applyRevisions.BumpDesired()
	if err != nil {
		log.Printf("apply subsystem: bump desired revision: %v", err)
		return 0
	}
	return rev
}

// executeApplyRevision applies one immutable desired revision to the runtime.
// It is the apply.ExecuteFunc seam for the Runner. IMPORTANT: callers invoke
// the Runner while already holding s.mu (via autoApplyResultLocked), so this
// function must NOT acquire s.mu again (the mutation that produced the
// revision is already committed and the apply runner serializes execution).
func (s *managementState) executeApplyRevision(revision uint64) (apply.Result, error) {
	response, status, err := NewApplyWorkflow(NewManagementApplyContext(s)).
		RunLocked(ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	res := apply.Result{Success: err == nil && status == http.StatusOK, RolledBack: response.RolledBack}
	if err != nil {
		res.ErrorCode = "APPLY_ERROR"
		res.ErrorMessage = err.Error()
		return res, err
	}
	if status != http.StatusOK {
		res.ErrorCode = applyFailureCode(response)
		res.ErrorMessage = applyFailureMessage(response, status)
	}
	return res, nil
}

// applyFailureCode maps an unsuccessful apply response to a stable error code.
func applyFailureCode(r ApplyResponse) string {
	if r.RolledBack {
		return "ROLLED_BACK"
	}
	for _, h := range r.HealthChecks {
		if !h.Healthy {
			return "HEALTH_CHECK_FAILED"
		}
	}
	for _, a := range r.ServiceActions {
		if !a.Success {
			return "SERVICE_ACTION_FAILED"
		}
	}
	for _, v := range r.Validations {
		if !v.Valid {
			return "VALIDATION_FAILED"
		}
	}
	return "APPLY_FAILED"
}

func applyFailureMessage(r ApplyResponse, status int) string {
	for _, h := range r.HealthChecks {
		if !h.Healthy {
			return "health check failed for " + h.Name + ": " + firstNonEmpty(h.Error, h.Output)
		}
	}
	for _, a := range r.ServiceActions {
		if !a.Success {
			return "service action failed for " + a.Name + ": " + firstNonEmpty(a.Error, a.Output)
		}
	}
	for _, v := range r.Validations {
		if !v.Valid {
			return "config validation failed for " + v.Name + ": " + firstNonEmpty(v.Error, v.Output)
		}
	}
	return "apply did not succeed (status " + http.StatusText(status) + ")"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
