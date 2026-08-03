package apply

import (
	"testing"
	"time"
)

func TestRecoveryDoesNotInventRestartPanelSuccessBeforeHelperEvidence(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	leases := NewLeaseStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, acquired, err := leases.Acquire("dead-owner", "restart-operation", now.Add(-time.Hour), time.Second)
	if err != nil || !acquired {
		t.Fatalf("acquire setup lease: acquired=%v err=%v", acquired, err)
	}
	job := Job{
		ID: "restart-before-helper", DesiredRevision: desired, BaseRevision: 0,
		Status: StatusApplying, Trigger: "panel-update-restart", ActorID: "system",
		CreatedAt: now.Add(-time.Hour).Unix(), OwnerProcess: lease.Owner, LeaseGeneration: lease.Generation,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	if err := recordRuntimePublicationIntent(db, job, lease, now.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := markRuntimePublicationPublishing(db, job.ID, lease.Generation, PublicationDetails{ServicePhase: "restart-panel"}, now.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("recovery must not rerun an unproven helper mutation")
		return Result{}, nil
	})
	defer runner.Close()
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusFailed || persisted.ErrorCode != "PUBLICATION_NOT_STARTED" {
		t.Fatalf("crash before helper was invented as success: %+v", persisted)
	}
	current, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.Applied != 0 {
		t.Fatalf("restart receipt advanced applied revision without helper evidence: %+v", current)
	}
}
