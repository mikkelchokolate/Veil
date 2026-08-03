package apply

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestIndependentRunnersShareDurableApplyLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.db")
	db1, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	rs1, js1 := NewRevisionStore(db1), NewJobStore(db1)
	rs2, js2 := NewRevisionStore(db2), NewJobStore(db2)
	revision, err := rs1.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	first := NewRunner(rs1, js1, func(uint64) (Result, error) {
		close(started)
		<-release
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	var secondExecutions atomic.Int32
	second := NewRunner(rs2, js2, func(uint64) (Result, error) {
		secondExecutions.Add(1)
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	firstDone := make(chan error, 1)
	go func() {
		_, runErr := first.Run(revision, "mutation", "first-process")
		firstDone <- runErr
	}()
	<-started

	if _, err := second.Run(revision, "retry", "second-process"); !errors.Is(err, ErrApplyBusy) {
		t.Errorf("second process was not rejected by durable lease: %v", err)
	}
	if got := secondExecutions.Load(); got != 0 {
		t.Errorf("second process touched runtime %d times while first lease was active", got)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-firstDone; err != nil {
		t.Fatalf("first run: %v", err)
	}
}

func TestNewRunnerMarksStaleApplyingJobsInterrupted(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	old := time.Now().Add(-time.Hour).Unix()
	job := Job{
		ID:              "stale-applying",
		DesiredRevision: 1,
		Status:          StatusApplying,
		Trigger:         "mutation",
		CreatedAt:       old,
		StartedAt:       &old,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	_ = NewRunner(NewRevisionStore(db), jobs, func(uint64) (Result, error) {
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	got, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.ErrorCode != "INTERRUPTED" || got.FinishedAt == nil {
		t.Fatalf("stale applying job remained active after startup: %+v", got)
	}
}

func TestJobStoreStatusUpdateChecksRowsAffected(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if err := NewJobStore(db).MarkStatus("missing-job", StatusApplying, "", ""); err == nil {
		t.Fatal("MarkStatus reported success for a missing job")
	}
}
