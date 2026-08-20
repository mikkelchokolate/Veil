package apply

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewRunnerClosesSupersededRecoveryPendingJob(t *testing.T) {
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
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at)
VALUES(?,?,?,?,?,?)`, job.ID, first, 1, "", "[]", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("superseded recovery must not re-run apply")
		return Result{}, nil
	})
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("startupErr = %v", err)
	}
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != second {
		t.Fatalf("applied = %d, want %d", state.Applied, second)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Terminal() {
		t.Fatalf("job still active: %+v", persisted)
	}
}

func TestRecoverRuntimePublicationDoesNotRewindAppliedRevision(t *testing.T) {
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
		Status: StatusFailed, Trigger: "retry", CreatedAt: time.Now().Unix(),
		OwnerProcess: "pid:1:stale", LeaseGeneration: 7,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase)
VALUES(?,?,?,?,?,?,?)`, job.ID, first, 7, "", "[]", time.Now().Unix(), "finalization_pending"); err != nil {
		t.Fatal(err)
	}
	leases := NewLeaseStore(db)
	if err := recoverRuntimePublications(db, leases, jobs, "pid:recovery", time.Now, 30*time.Second); err != nil {
		t.Fatalf("recoverRuntimePublications: %v", err)
	}
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != second {
		t.Fatalf("applied rewound to %d, want %d", state.Applied, second)
	}
}
