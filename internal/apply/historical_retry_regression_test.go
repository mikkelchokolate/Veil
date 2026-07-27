package apply

import "testing"

func TestRunnerRejectsHistoricalRetryWithoutTouchingRuntimeOrRevisions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)

	var revision10 uint64
	for i := 0; i < 10; i++ {
		var err error
		revision10, err = revisions.BumpDesired()
		if err != nil {
			t.Fatal(err)
		}
	}
	if revision10 != 10 {
		t.Fatalf("setup desired=%d want=10", revision10)
	}
	if err := revisions.MarkApplied(10); err != nil {
		t.Fatal(err)
	}

	executed := make([]uint64, 0, 1)
	runner := NewRunner(revisions, jobs, func(revision uint64) (Result, error) {
		executed = append(executed, revision)
		return Result{Success: true}, nil
	})
	before, err := jobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	job, err := runner.Run(3, "retry", "admin")
	if err == nil {
		t.Fatalf("historical retry unexpectedly succeeded: %+v", job)
	}
	if len(executed) != 0 {
		t.Fatalf("historical runtime executed revisions %v", executed)
	}
	afterRevision, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision.Desired != 10 || afterRevision.Applied != 10 {
		t.Fatalf("historical retry changed revision state: desired=%d applied=%d want=10/10", afterRevision.Desired, afterRevision.Applied)
	}
	after, err := jobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("historical retry created a job: before=%d after=%d", len(before), len(after))
	}
}

func TestRunnerRejectsNonCurrentDesiredRetryEvenWhenAboveApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	for i := 0; i < 10; i++ {
		if _, err := revisions.BumpDesired(); err != nil {
			t.Fatal(err)
		}
	}
	if err := revisions.MarkApplied(7); err != nil {
		t.Fatal(err)
	}

	executed := false
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		executed = true
		return Result{Success: true}, nil
	})
	if job, err := runner.Run(8, "retry", "admin"); err == nil {
		t.Fatalf("non-current desired retry unexpectedly succeeded: %+v", job)
	}
	if executed {
		t.Fatal("runtime executed a superseded desired revision")
	}
	state, _ := revisions.Get()
	if state.Desired != 10 || state.Applied != 7 {
		t.Fatalf("revision state changed: %+v", state)
	}
}
