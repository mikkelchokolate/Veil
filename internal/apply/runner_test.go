package apply

import (
	"context"
	"errors"
	"testing"
)

// fakeExecutor records the revision it was asked to apply and can be told to
// fail, simulating the underlying staged-apply pipeline.
type fakeExecutor struct {
	applied []uint64
	err     error
}

func (f *fakeExecutor) apply(rev uint64) (Result, error) {
	f.applied = append(f.applied, rev)
	if f.err != nil {
		return Result{ErrorCode: "HEALTH_CHECK_FAILED", ErrorMessage: f.err.Error()}, f.err
	}
	return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
}

func TestRunnerRunContextCancelsContextAwareExecutor(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		<-ctx.Done()
		return Result{ErrorCode: "canceled", ErrorMessage: ctx.Err().Error()}, ctx.Err()
	}))
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job, err := runner.RunContext(ctx, desired, "mutation", "admin")
	if !errors.Is(err, context.Canceled) || job.Status != StatusFailed {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestRunnerSuccessfulJobMarksApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	js := NewJobStore(db)
	exec := &fakeExecutor{}
	r := NewRunner(rs, js, exec.apply)

	desired, _ := rs.BumpDesired()
	job, err := r.Run(desired, "mutation", "admin")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("expected succeeded, got %s (%s)", job.Status, job.ErrorMessage)
	}
	rev, _ := rs.Get()
	if rev.Applied != desired {
		t.Fatalf("applied=%d want %d", rev.Applied, desired)
	}
	if len(exec.applied) != 1 || exec.applied[0] != desired {
		t.Fatalf("executor applied wrong revision: %v", exec.applied)
	}
}

func TestRunnerFailureLeavesAppliedUnchanged(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	js := NewJobStore(db)
	exec := &fakeExecutor{err: errors.New("boom")}
	r := NewRunner(rs, js, exec.apply)

	// Apply revision 1 successfully.
	d1, _ := rs.BumpDesired()
	if _, err := r.Run(d1, "mutation", "admin"); err == nil {
		// switch executor to succeed for rev1
	}
	exec.err = nil
	d2, _ := rs.BumpDesired()
	if _, err := r.Run(d2, "mutation", "admin"); err != nil {
		t.Fatalf("run rev2: %v", err)
	}
	rev, _ := rs.Get()
	if rev.Applied != d2 {
		t.Fatalf("setup: applied=%d want %d", rev.Applied, d2)
	}

	// Now a failing apply for revision 3 must not advance applied.
	exec.err = errors.New("health check failed")
	d3, _ := rs.BumpDesired()
	job, err := r.Run(d3, "mutation", "admin")
	if err == nil {
		t.Fatalf("expected error from failing apply")
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", job.Status)
	}
	rev, _ = rs.Get()
	if rev.Applied != d2 || rev.Desired != d3 {
		t.Fatalf("after failure: applied=%d desired=%d, want %d/%d", rev.Applied, rev.Desired, d2, d3)
	}
}

func TestRunnerSerializesConcurrentJobs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	js := NewJobStore(db)

	started := make(chan struct{})
	release := make(chan struct{})
	blocking := func(rev uint64) (Result, error) {
		close(started)
		<-release // hold the apply open so a concurrent Run sees "busy"
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}
	r := NewRunner(rs, js, blocking)

	d, _ := rs.BumpDesired()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(d, "mutation", "admin")
		done <- err
	}()
	<-started // first job is now executing and holds the runner

	d2, _ := rs.BumpDesired()
	_, err := r.Run(d2, "mutation", "admin")
	if !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("expected ErrApplyBusy while a job is active, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first job: %v", err)
	}
}

func TestRunnerRunLatestDrainsNewerDesiredAfterBusy(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	js := NewJobStore(db)

	started := make(chan struct{})
	release := make(chan struct{})
	applied := make([]uint64, 0, 2)
	exec := func(rev uint64) (Result, error) {
		if rev == 1 {
			close(started)
			<-release
		}
		applied = append(applied, rev)
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}
	r := NewRunner(rs, js, exec)

	if _, err := rs.BumpDesired(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.RunLatest(context.Background(), "mutation", "admin")
		done <- err
	}()
	<-started
	d2, err := rs.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("RunLatest: %v", err)
	}
	rev, err := rs.Get()
	if err != nil {
		t.Fatal(err)
	}
	if rev.Applied != d2 {
		t.Fatalf("applied=%d want %d (pending drain missed newer desired)", rev.Applied, d2)
	}
	if len(applied) != 2 || applied[0] != 1 || applied[1] != d2 {
		t.Fatalf("applied revisions = %v, want [1 %d]", applied, d2)
	}
}

func TestRunnerRejectsPinnedRevisionWhenItIsNoLongerCurrentDesired(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	js := NewJobStore(db)
	exec := &fakeExecutor{}
	r := NewRunner(rs, js, exec.apply)

	d1, _ := rs.BumpDesired()
	_, _ = rs.BumpDesired()
	if _, err := r.Run(d1, "retry", "admin"); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("historical retry error=%v want ErrStaleRevision", err)
	}
	if len(exec.applied) != 0 {
		t.Fatalf("historical retry touched runtime: %v", exec.applied)
	}
}
