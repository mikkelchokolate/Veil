package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/apply"
)

// registerApplyRoutes registers the durable apply workflow endpoints. These
// expose desired/applied revisions, apply job history, retry, and reconcile.
// Reads are available to viewers; mutations (retry/reconcile) require admin,
// enforced by the shared auth middleware for mutating methods.
func (s *managementState) registerApplyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/apply/state", s.handleApplyState)
	mux.HandleFunc("/api/apply/jobs", s.handleApplyJobs)
	mux.HandleFunc("/api/apply/jobs/", s.handleApplyJobByID)
	mux.HandleFunc("/api/apply/reconcile", s.handleApplyReconcile)
	mux.HandleFunc("/api/apply/rollback", s.handleApplyRollback)
}

// handleApplyState returns desired/applied revisions and the derived system
// state (synced/pending/applying/failed/rolling_back/rolled_back/degraded).
func (s *managementState) handleApplyState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.applyStateViewLocked())
}

// handleApplyJobs lists apply jobs, newest first.
func (s *managementState) handleApplyJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.applyTrackingEnabled() {
		writeJSON(w, map[string]any{"items": []apply.Job{}})
		return
	}
	limit := 50
	jobs, err := s.applyJobs.List(limit)
	if err != nil {
		writeError(w, "list apply jobs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []apply.Job{}
	}
	writeJSON(w, map[string]any{"items": jobs})
}

// handleApplyJobByID handles GET /api/apply/jobs/{id} and
// POST /api/apply/jobs/{id}/retry.
func (s *managementState) handleApplyJobByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/apply/jobs/")
	if rest == "" {
		writeNotFound(w)
		return
	}
	if strings.HasSuffix(rest, "/retry") {
		s.handleApplyJobRetry(w, r, strings.TrimSuffix(rest, "/retry"))
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.applyTrackingEnabled() {
		writeNotFound(w)
		return
	}
	job, err := s.applyJobs.Get(rest)
	if err != nil {
		writeNotFound(w)
		return
	}
	writeJSON(w, job)
}

// handleApplyJobRetry creates a NEW apply job for the same desired revision as
// the referenced job. It never rewrites the old job's history. Retry requires
// admin (mutating method, enforced by middleware) and an active apply must not
// be running.
func (s *managementState) handleApplyJobRetry(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.applyTrackingEnabled() {
		writeError(w, "apply tracking unavailable", http.StatusServiceUnavailable)
		return
	}
	orig, err := s.applyJobs.Get(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	actor, _ := r.Context().Value(contextKeyUsername).(string)

	s.mu.Lock()
	job, runErr := s.applyRunner.Run(orig.DesiredRevision, "retry", actor)
	after, _ := s.applyRevisions.Get()
	s.mu.Unlock()

	resp := map[string]any{
		"applyJob": job,
		"revision": map[string]any{
			"desired": after.Desired,
			"applied": after.Applied,
			"state":   deriveSystemState(after, &job),
		},
	}
	switch {
	case errors.Is(runErr, apply.ErrApplyBusy):
		writeError(w, "another apply job is active", http.StatusConflict)
		return
	case runErr != nil:
		// The job failed; report it honestly with the final job record.
		writeJSONStatus(w, http.StatusOK, resp)
		return
	}
	writeJSONStatus(w, http.StatusOK, resp)
}

// handleApplyReconcile applies the current desired revision if it is ahead of
// the applied revision. It is the operator action to converge the system after
// a failure or restart. Idempotent: if already synced it is a no-op.
func (s *managementState) handleApplyReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.applyTrackingEnabled() {
		writeError(w, "apply tracking unavailable", http.StatusServiceUnavailable)
		return
	}
	actor, _ := r.Context().Value(contextKeyUsername).(string)

	s.mu.Lock()
	rev, err := s.applyRevisions.Get()
	if err != nil {
		s.mu.Unlock()
		writeError(w, "read revisions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if rev.Desired == rev.Applied {
		state := s.applyStateViewLocked()
		s.mu.Unlock()
		writeJSON(w, map[string]any{"reconciled": false, "state": state})
		return
	}
	job, runErr := s.applyRunner.Run(rev.Desired, "reconcile", actor)
	after, _ := s.applyRevisions.Get()
	state := s.applyStateViewLocked()
	s.mu.Unlock()

	if errors.Is(runErr, apply.ErrApplyBusy) {
		writeError(w, "another apply job is active", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{
		"reconciled": runErr == nil,
		"applyJob":   job,
		"revision":   map[string]any{"desired": after.Desired, "applied": after.Applied},
		"state":      state,
	})
}
