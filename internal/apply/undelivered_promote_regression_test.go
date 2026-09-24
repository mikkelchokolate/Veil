package apply

import (
	"context"
	"errors"
	"testing"
)

// A dial-level helper failure proves the promote request was never delivered:
// no artifact could have been published. Such a job must finalize as a
// terminal failure and release the lease instead of pinning recovery_pending
// until a helper that may never return shows up.
func TestUndeliveredMutationFinalizesAsFailed(t *testing.T) {
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
			Artifacts:                  []string{"rules/geoip.dat"},
			LiveRoot:                   t.TempDir(),
		}); err != nil {
			t.Fatal(err)
		}
		return Result{
			Success:         false,
			RuntimeMutation: RuntimeMutationOutcome{MutationStarted: false},
		}, errors.New("privileged operation failed: dial unix /run/veil/helper.sock: connect: no such file or directory")
	}))
	defer runner.Close()
	job, err := runner.RunContext(context.Background(), desired, "mutation", "admin")
	if err == nil {
		t.Fatal("undelivered mutation was reported successful")
	}
	if job.Status != StatusFailed {
		t.Fatalf("undelivered mutation must finalize terminally, got %+v", job)
	}
	var receipts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("terminal failure must consume the publication receipt, retained %d", receipts)
	}
	current, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.Applied != 0 {
		t.Fatalf("failed mutation advanced applied revision: %+v", current)
	}

	// The lease must be released so the next apply can proceed.
	next, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	runner2 := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		return Result{
			Success:     true,
			Disposition: ApplyDispositionStaged,
		}, nil
	}))
	defer runner2.Close()
	second, err := runner2.RunContext(context.Background(), next, "mutation", "admin")
	if err != nil {
		t.Fatalf("apply after undelivered failure must not stay wedged: %v", err)
	}
	if second.Status != StatusStaged {
		t.Fatalf("follow-up apply status = %s, want staged", second.Status)
	}
}
