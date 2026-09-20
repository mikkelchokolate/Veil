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
	}
	// success means "the mutation is reflected in the runtime". An apply was
	// attempted only when auto-apply ran; its outcome decides the flag. When
	// no apply was required (auto-apply off) or the mutation needed none,
	// success stays true — but a failed apply is never reported as success
	// just because no durable job record exists (#536).
	obj["success"] = !outcome.attempted || outcome.success
	writeJSONStatus(w, status, obj)
}

// mergeOutcomeInto merges the mutation envelope (revision/applyJob/success)
// into an already-built response map, matching writeMutationResponse's shape.
func (s *managementState) mergeOutcomeInto(obj map[string]any, outcome autoApplyOutcome) {
	obj["revision"] = s.mergedRevisionView(outcome)
	if outcome.job != nil {
		obj["applyJob"] = outcome.job
	}
	// Same contract as writeMutationResponse: success is only forced true
	// when no apply was attempted (#536).
	obj["success"] = !outcome.attempted || outcome.success
}

// mergedRevisionView resolves the revision view for a mutation response.
func (s *managementState) mergedRevisionView(outcome autoApplyOutcome) revisionView {
	if !s.applyTrackingEnabled() {
		// Tracking is off: there is no durable evidence the runtime converged
		// — report untracked rather than a false-green "synced" (#539).
		return revisionView{State: apply.StateUntracked}
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
