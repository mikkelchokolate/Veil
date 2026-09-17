package apply

import (
	"context"
	"errors"
	"testing"
)

// Regression for #310: when a committed runtime mutation fails with an
// incomplete rollback, the job stays recovery_pending — and the attempted
// operation breakdown must be persisted with that transition so a refreshed
// job detail keeps the failed attempt's diagnostics.
func TestRecoveryPendingPersistsOperationBreakdown(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	attempt := []OperationResult{
		{Type: "promote", Target: "caddy/config.json", Success: true},
		{Type: "service", Target: "veil-hysteria2@edge.service", Success: false, Detail: "start failed"},
	}
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		if err := MarkRuntimeMutationStarting(ctx, PublicationDetails{
			ExpectedLiveManifestSHA256: "expected",
			PreviousLiveManifestSHA256: "previous",
			Artifacts:                  []string{"config.json"},
			LiveRoot:                   t.TempDir(),
		}); err != nil {
			t.Fatal(err)
		}
		return Result{
			Success: false,
			RuntimeMutation: RuntimeMutationOutcome{
				MutationStarted:  true,
				ArtifactsChanged: true,
				Ambiguous:        true,
			},
			Operations: attempt,
		}, errors.New("health check failed after mutation")
	}))
	defer runner.Close()
	job, err := runner.RunContext(context.Background(), desired, "mutation", "admin")
	if err == nil {
		t.Fatal("ambiguous mutation was reported successful")
	}
	if job.Status != StatusRecoveryPending {
		t.Fatalf("job status = %q, want recovery_pending", job.Status)
	}
	if len(job.Operations) != len(attempt) {
		t.Fatalf("returned job lost the operation breakdown: %+v", job.Operations)
	}

	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusRecoveryPending {
		t.Fatalf("persisted status = %q, want recovery_pending", persisted.Status)
	}
	if len(persisted.Operations) != len(attempt) {
		t.Fatalf("persisted job lost the operation breakdown: %+v", persisted.Operations)
	}
	for i, op := range persisted.Operations {
		if op != attempt[i] {
			t.Fatalf("persisted operation %d = %+v, want %+v", i, op, attempt[i])
		}
	}
	listed, err := jobs.List(10)
	if err != nil {
		t.Fatal(err)
	}
	var listedJob *Job
	for i := range listed {
		if listed[i].ID == job.ID {
			listedJob = &listed[i]
		}
	}
	if listedJob == nil || len(listedJob.Operations) != len(attempt) {
		t.Fatalf("listed job lost the operation breakdown: %+v", listedJob)
	}
}
