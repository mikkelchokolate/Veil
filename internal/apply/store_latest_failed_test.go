package apply

import "testing"

// #546: LatestFailed must surface recovery_pending jobs — a job whose
// rollback could not complete is unresolved failure evidence; hiding it from
// lastFailedJobId would let the state view claim health.
func TestLatestFailedIncludesRecoveryPending(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)

	recovery := Job{ID: "job-recovery", DesiredRevision: 1, Status: StatusPending, Trigger: "manual", CreatedAt: 100}
	if err := jobs.Create(recovery); err != nil {
		t.Fatal(err)
	}
	if err := jobs.MarkStatus(recovery.ID, StatusRecoveryPending, "ROLLBACK_FAILED", "firewall rollback failed"); err != nil {
		t.Fatal(err)
	}

	got, ok, err := jobs.LatestFailed()
	if err != nil || !ok {
		t.Fatalf("LatestFound=%v err=%v — recovery_pending must count as failed", ok, err)
	}
	if got.ID != recovery.ID || got.ErrorCode != "ROLLBACK_FAILED" {
		t.Fatalf("LatestFailed = %+v, want the recovery_pending job with its error", got)
	}
}

// A recovery_pending job whose error was transferred to a superseding job is
// bookkeeping, not a live failure — it stays excluded.
func TestLatestFailedSkipsTransferredRecovery(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)

	transferred := Job{ID: "job-transferred", DesiredRevision: 1, Status: StatusPending, Trigger: "manual", CreatedAt: 100}
	if err := jobs.Create(transferred); err != nil {
		t.Fatal(err)
	}
	if err := jobs.MarkStatus(transferred.ID, StatusRecoveryPending, "PUBLICATION_RECOVERY_TRANSFERRED", "superseded"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := jobs.LatestFailed(); err != nil || ok {
		t.Fatalf("transferred recovery must not surface as failed: ok=%v err=%v", ok, err)
	}
}
