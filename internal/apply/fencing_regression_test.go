package apply

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestExpiredLeaseOwnerCannotFinalizeAfterSuccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fencing.db")
	dbA, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dbA.Close()
	dbB, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dbB.Close()

	revisionsA, jobsA := NewRevisionStore(dbA), NewJobStore(dbA)
	revisionsB, jobsB := NewRevisionStore(dbB), NewJobStore(dbB)
	revision, err := revisionsA.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}

	startedA := make(chan struct{})
	resumeA := make(chan struct{})
	var resumeOnce sync.Once
	defer resumeOnce.Do(func() { close(resumeA) })

	runnerA := NewRunner(revisionsA, jobsA, ContextExecutorFunc(func(context.Context, uint64) (Result, error) {
		close(startedA)
		<-resumeA
		return Result{Success: true, Operations: []OperationResult{{Type: "runtime", Target: "process-a", Success: true}}}, nil
	}))
	runnerB := NewRunner(revisionsB, jobsB, ContextExecutorFunc(func(context.Context, uint64) (Result, error) {
		return Result{Success: true, Operations: []OperationResult{{Type: "runtime", Target: "process-b", Success: true}}}, nil
	}))

	base := time.Unix(2_000_000_000, 0).UTC()
	runnerA.now = func() time.Time { return base }
	runnerA.leaseTTL = time.Second
	runnerA.heartbeatInterval = time.Hour
	runnerB.now = func() time.Time { return base.Add(2 * time.Second) }
	runnerB.leaseTTL = time.Second
	runnerB.heartbeatInterval = time.Hour

	type runResult struct {
		job Job
		err error
	}
	aDone := make(chan runResult, 1)
	go func() {
		job, runErr := runnerA.RunContext(context.Background(), revision, "mutation", "process-a")
		aDone <- runResult{job: job, err: runErr}
	}()
	<-startedA

	jobB, err := runnerB.RunContext(context.Background(), revision, "retry", "process-b")
	if err != nil {
		t.Fatalf("successor process failed to acquire expired lease: %v", err)
	}
	if jobB.Status != StatusSucceeded {
		t.Fatalf("successor status = %s, want succeeded", jobB.Status)
	}

	resumeOnce.Do(func() { close(resumeA) })
	resultA := <-aDone
	if resultA.err == nil {
		t.Errorf("stale process A reported success after process B acquired a newer lease: %+v", resultA.job)
	}
	persistedA, err := jobsA.Get(resultA.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedA.Status == StatusSucceeded {
		t.Errorf("stale process A persisted a succeeded job after fencing: %+v", persistedA)
	}
	persistedB, err := jobsB.Get(jobB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedB.Status != StatusSucceeded {
		t.Errorf("successor process B did not remain the sole successful owner: %+v", persistedB)
	}
}

func TestHeartbeatLossImmediatelyCancelsExecutorContext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	forcedStop := make(chan struct{})
	var stopOnce sync.Once
	defer stopOnce.Do(func() { close(forcedStop) })
	executorResult := make(chan error, 1)
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		close(started)
		select {
		case <-ctx.Done():
			executorResult <- ctx.Err()
			return Result{}, ctx.Err()
		case <-forcedStop:
			executorResult <- errors.New("test forced executor shutdown")
			return Result{}, errors.New("test forced executor shutdown")
		}
	}))
	runner.leaseTTL = 5 * time.Second
	runner.heartbeatInterval = 10 * time.Millisecond

	runDone := make(chan error, 1)
	go func() {
		_, runErr := runner.RunContext(context.Background(), revision, "mutation", "heartbeat-owner")
		runDone <- runErr
	}()
	<-started

	// Steal the row without waiting for expiry. The next heartbeat deterministically
	// affects zero rows and reports ErrApplyLeaseLost. The runner must cancel the
	// exact context observed by the executor immediately.
	if _, err := db.Exec(`UPDATE apply_lease SET owner_process='successor', lease_expires_at=? WHERE id=1`, time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}

	select {
	case executorErr := <-executorResult:
		if !errors.Is(executorErr, context.Canceled) {
			t.Errorf("executor stopped for %v, want context cancellation from heartbeat loss", executorErr)
		}
	case <-time.After(500 * time.Millisecond):
		stopOnce.Do(func() { close(forcedStop) })
		<-executorResult
		t.Error("executor context was not canceled promptly after durable heartbeat loss")
	}

	select {
	case runErr := <-runDone:
		if runErr == nil || !errors.Is(runErr, ErrApplyLeaseLost) {
			t.Errorf("run error = %v, want durable lease loss", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not return after executor cancellation")
	}
}
