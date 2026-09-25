package apply

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustCreateJob(t *testing.T, jobs *JobStore, job Job) Job {
	t.Helper()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.CreatedAt == 0 {
		job.CreatedAt = time.Now().Unix()
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	return job
}

func mustInsertReceipt(t *testing.T, db *sql.DB, jobID string, revision uint64, generation uint64, phase string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase)
VALUES(?,?,?,?,?,?,?)`, jobID, revision, generation, "", "[]", time.Now().Unix(), phase); err != nil {
		t.Fatal(err)
	}
}

func receiptCount(t *testing.T, db *sql.DB, jobID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, jobID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSupersededRecoveryPendingConsumesReceiptAndLease(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
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
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: first, BaseRevision: first,
		Status: StatusRecoveryPending, Trigger: "retry", ActorID: "system",
		OwnerProcess: "pid:1:stale", LeaseGeneration: 1,
	})
	// A mid-flight phase keeps the receipt unresolved until the superseded
	// close consumes it (#1034).
	mustInsertReceipt(t, db, job.ID, first, 1, "services_planned")

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("superseded recovery must not re-run apply")
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
	if persisted.Status != StatusFailed || persisted.ErrorCode != "SUPERSEDED" {
		t.Fatalf("superseded close = %s/%s: %+v", persisted.Status, persisted.ErrorCode, persisted)
	}
	if got := receiptCount(t, db, job.ID); got != 0 {
		t.Fatalf("immortal receipt survived the superseded close: %d rows", got)
	}
	var archived int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publication_history WHERE job_id=? AND final_phase='superseded'`, job.ID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("superseded close did not archive receipt evidence: %d rows", archived)
	}
	lease, err := NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner != "" {
		t.Fatalf("durable lease still held after superseded close: %+v", lease)
	}
	// A second recovery pass must be a no-op: the job must not flip-flop
	// back to recovery_pending (#1034).
	if err := recoverRuntimePublications(db, NewLeaseStore(db), jobs, "pid:recovery-second-pass", time.Now, 30*time.Second); err != nil {
		t.Fatalf("second recovery pass: %v", err)
	}
	persisted, err = jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusFailed {
		t.Fatalf("closed job resurrected to %q by second recovery pass", persisted.Status)
	}
}

func TestRecoverRuntimePublicationsConsumesReceiptForTerminalJob(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	// A job that already finished (e.g. closed superseded before the
	// receipt-consume fix) must not be resurrected by its leftover receipt.
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: desired, Status: StatusFailed,
		ErrorCode: "INTERRUPTED", OwnerProcess: "pid:1:stale", LeaseGeneration: 4,
	})
	mustInsertReceipt(t, db, job.ID, desired, 4, "finalization_pending")

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	defer runner.Close()
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("startupErr = %v", err)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusFailed {
		t.Fatalf("terminal job was resurrected to %q", persisted.Status)
	}
	if got := receiptCount(t, db, job.ID); got != 0 {
		t.Fatalf("terminal-job receipt survived recovery: %d rows", got)
	}
	var archived int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publication_history WHERE job_id=?`, job.ID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatal("terminal-job receipt was dropped without archiving evidence")
	}
	lease, err := NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner != "" {
		t.Fatalf("lease retained after terminal receipt consume: %+v", lease)
	}
}

func TestRecoverPublishedReceiptSkipsStaleEnforcementConfirmation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: desired, Status: StatusApplying,
		Trigger: "mutation", OwnerProcess: "pid:1:stale", LeaseGeneration: 2,
	})
	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,quota_reset_policy,notes,depleted,created_at,updated_at,version)
VALUES('c-stale','stale',1,'never','',0,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	// The enforcement row the persisted confirmation targets was superseded
	// by a newer plan before recovery could replay it (#1035).
	if _, err := db.Exec(`INSERT INTO quota_enforcement(client_id,target_generation,target_payload_hash,target_depleted,state,next_retry_at,last_error,attempts,updated_at)
VALUES('c-stale',7,?,1,'superseded',0,'',0,1)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, confirmations_json, published_at, phase)
VALUES(?,?,?,?,'[]',?,?,?)`, job.ID, desired, 2, "",
		`[{"kind":"quota","clientId":"c-stale","targetGeneration":7,"targetPayloadHash":"`+strings.Repeat("a", 64)+`"}]`,
		time.Now().Unix(), "published"); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("published receipt recovery must not re-run apply")
		return Result{}, nil
	})
	defer runner.Close()
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("stale confirmation wedged recovery: %v", err)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusSucceeded {
		t.Fatalf("published receipt job status=%q, want %q", persisted.Status, StatusSucceeded)
	}
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != desired {
		t.Fatalf("applied=%d, want %d after published receipt finalize", state.Applied, desired)
	}
	if got := receiptCount(t, db, job.ID); got != 0 {
		t.Fatalf("published receipt was not consumed: %d rows", got)
	}
}

func TestRecoverPublishedReceiptWithCoveredRevisionIsConsumed(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := revisions.MarkApplied(desired); err != nil {
		t.Fatal(err)
	}
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: desired, Status: StatusApplying,
		Trigger: "mutation", OwnerProcess: "pid:1:stale", LeaseGeneration: 2,
	})
	mustInsertReceipt(t, db, job.ID, desired, 2, "published")

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("covered published receipt must not re-run apply")
		return Result{}, nil
	})
	defer runner.Close()
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("startupErr = %v", err)
	}
	if got := receiptCount(t, db, job.ID); got != 0 {
		// Before the fix the consume guard only matched 'artifacts_committed',
		// so this 'published' receipt was re-finalized on every pass (#1035).
		t.Fatalf("covered published receipt survived recovery: %d rows", got)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusSucceeded {
		t.Fatalf("covered receipt job status=%q, want %q", persisted.Status, StatusSucceeded)
	}
}

func TestSideEffectRecoveryPendingExpiresWithoutHelperEvidence(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	// Helper evidence stuck at commitPhase='intent' wedges recovery forever
	// unless the wait is bounded (#1036).
	dir := t.TempDir()
	manifest := filepath.Join(dir, ".veil-update-evidence.json")
	if err := os.WriteFile(manifest,
		[]byte(`{"version":1,"transactionId":"tx-1","targetVersion":"9.9.9","commitPhase":"intent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: desired, Status: StatusApplying,
		Trigger: "panel-update", OwnerProcess: "pid:1:stale", LeaseGeneration: 3,
	})
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase, service_phase)
VALUES(?,?,?,?,'[]',?,'side_effect_planned','update-install')`, job.ID, desired, 3, "", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at)
VALUES(?,?,?,?,?)`, job.ID, "side_effect_planned", 3,
		fmt.Sprintf(`{"updateTransactionId":"tx-1","targetVersion":"9.9.9","activationManifest":%q}`, manifest),
		time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("side-effect recovery must not re-run apply")
		return Result{}, nil
	})
	defer runner.Close()
	readiness := runner.ReadinessError()
	if readiness == nil || !strings.Contains(readiness.Error(), "helper-owned commit evidence") {
		t.Fatalf("expected bounded evidence wait, got readiness=%v", readiness)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusRecoveryPending {
		t.Fatalf("unexpired evidence job status=%q, want %q", persisted.Status, StatusRecoveryPending)
	}
	if got := receiptCount(t, db, job.ID); got != 1 {
		t.Fatalf("unexpired evidence receipt was consumed early: %d rows", got)
	}

	// Once the grace window lapses, recovery must abandon the wedge instead
	// of retrying the evidence check forever (#1036).
	if _, err := db.Exec(`UPDATE apply_jobs SET created_at=? WHERE id=?`,
		time.Now().Unix()-sideEffectEvidenceGraceSeconds-1, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := runner.resumeRecoveryPending(context.Background()); err != nil {
		t.Fatalf("expired side-effect resume: %v", err)
	}
	persisted, err = jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusFailed || persisted.ErrorCode != "SIDE_EFFECT_EVIDENCE_EXPIRED" {
		t.Fatalf("expired evidence close = %s/%s: %+v", persisted.Status, persisted.ErrorCode, persisted)
	}
	if got := receiptCount(t, db, job.ID); got != 0 {
		t.Fatalf("expired evidence receipt survived: %d rows", got)
	}
	lease, err := NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner != "" {
		t.Fatalf("lease retained after evidence expiry close: %+v", lease)
	}
}

func TestRecoverRuntimePublicationsProcessesMultipleReceiptsInOnePass(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	first, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	second, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	older := time.Now().Add(-time.Minute).Unix()
	jobA := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: first, Status: StatusApplying,
		Trigger: "mutation", CreatedAt: older, OwnerProcess: "pid:1:stale", LeaseGeneration: 5,
	})
	jobB := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: second, Status: StatusApplying,
		Trigger: "mutation", OwnerProcess: "pid:1:stale", LeaseGeneration: 5,
	})
	// Both receipts need the lease-transfer path, which deliberately retains
	// the lease between receipts; the second Acquire must reuse it instead of
	// failing with ErrApplyBusy (#1040).
	mustInsertReceipt(t, db, jobA.ID, first, 5, "services_planned")
	mustInsertReceipt(t, db, jobB.ID, second, 5, "services_planned")

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	defer runner.Close()
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("two-receipt recovery pass aborted: %v", err)
	}
	for _, job := range []Job{jobA, jobB} {
		persisted, err := jobs.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Status != StatusRecoveryPending || persisted.OwnerProcess != runner.ownerID {
			t.Fatalf("receipt %s was not transferred to recovery: %+v", job.ID, persisted)
		}
	}
	lease, err := NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner != runner.ownerID {
		t.Fatalf("retained lease owner=%q, want %q", lease.Owner, runner.ownerID)
	}
	// A repeat pass over already-transferred receipts must stay idempotent.
	if err := recoverRuntimePublications(db, NewLeaseStore(db), jobs, runner.ownerID, time.Now, 30*time.Second); err != nil {
		t.Fatalf("repeat recovery pass over owned receipts: %v", err)
	}
}

func TestStartupRecoveryFailureHealsViaMonitor(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	if _, err := revisions.BumpDesired(); err != nil {
		t.Fatal(err)
	}
	job := mustCreateJob(t, jobs, Job{
		ID: uuid.NewString(), DesiredRevision: 1, Status: StatusApplying,
		Trigger: "mutation", OwnerProcess: "pid:1:stale", LeaseGeneration: 1,
	})
	// A receipt whose snapshot digest cannot match makes recoverStartup fail;
	// a transient failure like this must not pin startupErr for the process
	// lifetime (#1038).
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase)
VALUES(?,?,?,?,'[]',?,'intent')`, job.ID, 1, 1, strings.Repeat("0", 64), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	if err := runner.ReadinessError(); err == nil {
		t.Fatal("poisoned receipt produced no startup error")
	}
	if _, err := db.Exec(`DELETE FROM runtime_publications WHERE job_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	runner.startMonitorForTest()
	defer runner.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := runner.ReadinessError(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("monitor never cleared transient startup error: %v", runner.ReadinessError())
		}
		time.Sleep(25 * time.Millisecond)
	}
}
