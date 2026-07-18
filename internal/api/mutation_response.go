package api

import (
	"encoding/json"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/apply"
)

// revisionView is the desired/applied revision state surfaced alongside a
// mutation response. State is the derived system state
// (synced/pending/applying/failed/...).
type revisionView struct {
	Desired uint64 `json:"desired"`
	Applied uint64 `json:"applied"`
	State   string `json:"state"`
}

// writeMutationResponse writes a mutated object as-is (top-level) so existing
// clients and SDKs that decode the object keep working, and merges in the
// desired/applied revision state plus the resulting apply job as additive
// fields. Apply failures are surfaced via "success":false and the job record —
// never silently dropped. Extra keys are ignored by Go JSON decoders and most
// clients, preserving backward compatibility.
func (s *managementState) writeMutationResponse(w http.ResponseWriter, status int, data any, outcome autoApplyOutcome) {
	body, err := json.Marshal(data)
	if err != nil {
		writeError(w, "encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	obj := map[string]any{}
	if err := json.Unmarshal(body, &obj); err != nil {
		// Non-object payloads (arrays, scalars): fall back to writing as-is.
		writeJSONStatus(w, status, data)
		return
	}

	obj["revision"] = s.mergedRevisionView(outcome)
	if outcome.job != nil {
		obj["applyJob"] = outcome.job
		// An apply ran: report its outcome honestly. success=false means the
		// object was saved (desired) but is NOT yet live (applied).
		obj["success"] = outcome.success
	} else {
		obj["success"] = true
	}
	writeJSONStatus(w, status, obj)
}

// mergedRevisionView resolves the revision view for a mutation response.
func (s *managementState) mergedRevisionView(outcome autoApplyOutcome) revisionView {
	if !s.applyTrackingEnabled() {
		return revisionView{State: apply.StateSynced}
	}
	rev := outcome.revision
	if rev.Desired == 0 && rev.Applied == 0 {
		if cur, err := s.applyRevisions.Get(); err == nil {
			rev = cur
		}
	}
	var job *apply.Job
	if outcome.job != nil {
		job = outcome.job
	} else if jobs, err := s.applyJobs.List(1); err == nil && len(jobs) > 0 {
		job = &jobs[0]
	}
	return revisionView{Desired: rev.Desired, Applied: rev.Applied, State: deriveSystemState(rev, job)}
}

// currentRevisionView returns the revision view without running an apply.
func (s *managementState) currentRevisionView() revisionView {
	if !s.applyTrackingEnabled() {
		return revisionView{State: apply.StateSynced}
	}
	rev, err := s.applyRevisions.Get()
	if err != nil {
		return revisionView{State: apply.StateSynced}
	}
	jobs, _ := s.applyJobs.List(1)
	var latest *apply.Job
	if len(jobs) > 0 {
		latest = &jobs[0]
	}
	return revisionView{Desired: rev.Desired, Applied: rev.Applied, State: deriveSystemState(rev, latest)}
}
