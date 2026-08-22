package apply

import (
	"context"
	"errors"
	"testing"
)

func TestEmptyOperationListDoesNotProveRollback(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
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
			Operations: nil,
		}, errors.New("executor response lost after privileged mutation")
	}))
	defer runner.Close()
	job, err := runner.RunContext(context.Background(), desired, "mutation", "admin")
	if err == nil {
		t.Fatal("ambiguous mutation was reported successful")
	}
	if job.Status != StatusRecoveryPending || job.ErrorCode != "RECOVERY_PENDING" {
		t.Fatalf("ambiguous empty-operation mutation became terminal: %+v", job)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusRecoveryPending || persisted.ErrorCode != "FINALIZATION_PENDING" {
		t.Fatalf("recovery evidence was not retained: %+v", persisted)
	}
	var receipts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("ambiguous mutation retained %d publication receipts, want 1", receipts)
	}
	current, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.Applied != 0 {
		t.Fatalf("ambiguous mutation advanced applied revision: %+v", current)
	}
}
