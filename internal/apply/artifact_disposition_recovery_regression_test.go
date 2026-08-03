package apply

import "testing"

func TestArtifactsCommittedDispositionNeverRecoversAsRuntimeConverged(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{
			Success:     true,
			Disposition: ApplyDispositionArtifactsCommitted,
			RuntimeMutation: RuntimeMutationOutcome{
				MutationStarted:  true,
				ArtifactsChanged: true,
			},
		}, nil
	})
	job, err := runner.Run(desired, "manual", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("artifact-only job status=%q", job.Status)
	}
	beforeRestart, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if beforeRestart.Applied != 0 {
		t.Fatalf("artifact-only publication advanced applied revision before restart: %+v", beforeRestart)
	}
	runner.Close()

	recoveryRunner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("artifact-only recovery must not execute an unproven runtime apply")
		return Result{}, nil
	})
	defer recoveryRunner.Close()
	afterRestart, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.Applied != 0 {
		t.Fatalf("artifact-only receipt became applied during recovery: %+v", afterRestart)
	}
}
