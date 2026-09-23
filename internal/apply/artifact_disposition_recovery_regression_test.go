package apply

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestArtifactsCommittedDispositionNeverRecoversAsRuntimeConverged proves the
// artifact-only disposition is recorded as exactly that: a ContextExecutor
// (strict durable phases) commits artifacts, the receipt finalizes with an
// artifact-only terminal phase, the applied revision stays put, and a restart
// never runs an unproven runtime convergence against it.
func TestArtifactsCommittedDispositionNeverRecoversAsRuntimeConverged(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		for _, phase := range []string{
			PublicationPhaseArtifactsPrepared,
			PublicationPhaseArtifactsCommitted,
		} {
			if err := AdvanceRuntimePublication(ctx, phase, PublicationDetails{}); err != nil {
				return Result{}, err
			}
		}
		return Result{
			Success:     true,
			Disposition: ApplyDispositionArtifactsCommitted,
			RuntimeMutation: RuntimeMutationOutcome{
				MutationStarted:  true,
				ArtifactsChanged: true,
			},
		}, nil
	}))
	job, err := runner.Run(desired, "manual", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("artifact-only job status=%q", job.Status)
	}
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != 0 {
		t.Fatalf("artifact-only publication advanced applied revision: %+v", state)
	}

	// The durable journal must record the receipt as artifact-only: the live
	// receipt is consumed and the archived history entry carries the
	// artifacts_finalized terminal phase and an artifacts_committed
	// disposition — nothing here may be re-read as runtime convergence.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("artifact-only job left %d live publication receipts", count)
	}
	var finalPhase, disposition string
	if err := db.QueryRow(`SELECT final_phase,json_extract(receipt_json,'$.disposition')
FROM runtime_publication_history WHERE job_id=?`, job.ID).Scan(&finalPhase, &disposition); err != nil {
		t.Fatalf("artifact-only job left no archived journal: %v", err)
	}
	if finalPhase != "artifacts_finalized" {
		t.Fatalf("artifact-only journal final_phase=%q, want artifacts_finalized", finalPhase)
	}
	if disposition != string(ApplyDispositionArtifactsCommitted) {
		t.Fatalf("artifact-only journal disposition=%q, want %q", disposition, ApplyDispositionArtifactsCommitted)
	}
	runner.Close()

	// Restart against the same durable state: recovery classification must
	// leave the job untouched and never invoke the executor — there is no
	// runtime convergence to resume, proven or otherwise.
	var executorCalls atomic.Int32
	recoveryRunner := NewRunner(revisions, jobs, ContextExecutorFunc(func(context.Context, uint64) (Result, error) {
		executorCalls.Add(1)
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}))
	defer recoveryRunner.Close()
	if err := recoveryRunner.ReadinessError(); err != nil {
		t.Fatalf("recovery classification failed for artifact-only job: %v", err)
	}
	if err := recoveryRunner.resumeRecoveryPending(context.Background()); err != nil {
		t.Fatalf("resume after artifact-only job: %v", err)
	}
	if got := executorCalls.Load(); got != 0 {
		t.Fatalf("artifact-only recovery ran the executor %d times, want 0", got)
	}
	afterRestart, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.Applied != 0 {
		t.Fatalf("artifact-only receipt became applied during recovery: %+v", afterRestart)
	}
	recovered, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusSucceeded || recovered.ErrorCode != "" {
		t.Fatalf("artifact-only job was reopened by recovery: %+v", recovered)
	}
}
