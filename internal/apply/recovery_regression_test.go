package apply

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunnerRecoversPendingJobWithoutValidLease(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "abandoned-pending", DesiredRevision: revision, Status: StatusPending, Trigger: "mutation", CreatedAt: time.Now().Add(-time.Minute).Unix()}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	_ = NewRunner(revisions, jobs, func(uint64) (Result, error) { return Result{Success: true}, nil })
	got, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.ErrorCode != "INTERRUPTED" || got.FinishedAt == nil {
		t.Errorf("abandoned pending job was not recovered: %+v", got)
	}
}

func TestRunnerRecoversApplyingJobWhenLeaseExpiresAfterStartup(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	started := now.Unix()
	job := Job{ID: "expires-after-startup", DesiredRevision: revision, Status: StatusApplying, Trigger: "mutation", CreatedAt: started, StartedAt: &started}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	lease := NewLeaseStore(db)
	_, acquired, err := lease.Acquire(fmt.Sprintf("pid:%d:expires-after-startup", os.Getpid()), "test", now, time.Second)
	if err != nil || !acquired {
		t.Fatalf("seed lease: acquired=%v err=%v", acquired, err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) { return Result{Success: true}, nil })
	defer func() {
		if closeMethod := reflect.ValueOf(runner).MethodByName("Close"); closeMethod.IsValid() {
			closeMethod.Call(nil)
		}
	}()
	deadline := time.Now().Add(3500 * time.Millisecond)
	for {
		got, err := jobs.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == StatusFailed && got.ErrorCode == "INTERRUPTED" && got.FinishedAt != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired post-startup lease left job active: %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunnerRecordsRuntimePublicationRecoveryFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease := NewLeaseStore(db)
	_, acquired, err := lease.Acquire(fmt.Sprintf("pid:%d:recovery-error", os.Getpid()), "test", now, 2*time.Second)
	if err != nil || !acquired {
		t.Fatalf("seed lease: acquired=%v err=%v", acquired, err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) { return Result{Success: true}, nil })
	defer runner.Close()
	if _, err := db.Exec(`DROP TABLE runtime_publications`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		runner.mu.Lock()
		startupErr := runner.startupErr
		runner.mu.Unlock()
		if startupErr != nil {
			if !strings.Contains(startupErr.Error(), "runtime_publications") {
				t.Fatalf("unexpected startup recovery error: %v", startupErr)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime publication recovery error was not retained")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := runner.Run(revision, "retry", "test"); err == nil || !strings.Contains(err.Error(), "runtime_publications") {
		t.Fatalf("subsequent apply did not fail closed with startup recovery error: %v", err)
	}
}

func TestRunnerRecoversJobOwnedByDeadProcessBeforeLeaseExpiry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Minute).Unix()
	job := Job{ID: "dead-process", DesiredRevision: revision, Status: StatusApplying, Trigger: "mutation", CreatedAt: started, StartedAt: &started}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	deadPID := 999999
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", deadPID)); err == nil {
		t.Skipf("test PID %d unexpectedly exists", deadPID)
	}
	if _, err := db.Exec(`UPDATE apply_lease SET owner_process=?, lease_expires_at=?, heartbeat_at=?, current_operation='test' WHERE id=1`,
		fmt.Sprintf("pid:%d:dead-owner", deadPID), time.Now().Add(time.Hour).Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	_ = NewRunner(revisions, jobs, func(uint64) (Result, error) { return Result{Success: true}, nil })
	got, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.ErrorCode != "INTERRUPTED" || got.FinishedAt == nil {
		t.Errorf("dead process job was not recovered: %+v", got)
	}
}
