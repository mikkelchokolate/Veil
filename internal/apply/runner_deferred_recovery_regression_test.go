package apply

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNewRunnerDeferredStartupRecoverySkipsInlineResume covers #1067: the
// deferred constructor is used by management-state reloads that run under a
// mutex the apply executor itself needs (it locks managementState.mu to load
// the pinned revision snapshot). The constructor must not run a synchronous
// recovery-pending resume — the superseded cleanup below is the exact pass
// the synchronous constructor performs, and a deferred runner must leave the
// job untouched for its monitor goroutine.
func TestNewRunnerDeferredStartupRecoverySkipsInlineResume(t *testing.T) {
	db := openTestDB(t)
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	first, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	second, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := revisions.MarkApplied(second); err != nil {
		t.Fatal(err)
	}
	job := Job{
		ID: uuid.NewString(), DesiredRevision: first, BaseRevision: first,
		Status: StatusRecoveryPending, Trigger: "retry", ActorID: "system",
		CreatedAt: time.Now().Unix(), OwnerProcess: "pid:1:stale", LeaseGeneration: 1,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase)
VALUES(?,?,?,?,?,?,?)`, job.ID, first, 1, "", "[]", time.Now().Unix(), "services_planned"); err != nil {
		t.Fatal(err)
	}

	runner := NewRunnerDeferredStartupRecovery(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("deferred startup recovery must not invoke the executor inline")
		return Result{}, nil
	})
	defer runner.Close()
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("startupErr = %v", err)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusRecoveryPending {
		t.Fatalf("deferred construction resolved recovery job to %q synchronously; the resume must be left to the monitor", persisted.Status)
	}
}
