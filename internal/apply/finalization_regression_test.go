package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestRuntimeSuccessFinalizationIsAtomic(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER fail_success_terminal_write
BEFORE UPDATE OF status ON apply_jobs
WHEN NEW.status = 'succeeded'
BEGIN
  SELECT RAISE(ABORT, 'injected terminal job-store failure');
END;`); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		if err := markTestRuntimeConverged(ctx); err != nil {
			return Result{}, err
		}
		return Result{
			Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true,
			Operations: []OperationResult{{
				Type: "promote", Target: "runtime", Success: true,
			}},
		}, nil
	}))
	job, runErr := runner.RunContext(context.Background(), revision, "mutation", "actor")
	if runErr == nil {
		t.Error("injected finalization failure was reported as an unqualified success")
	}

	current, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.Applied != 0 {
		t.Errorf("applied_revision advanced despite failed atomic finalization: %d", current.Applied)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusRecoveryPending || persisted.ErrorCode != "FINALIZATION_PENDING" {
		t.Errorf("unresolved finalization was not durably retained: %+v", persisted)
	}
	if persisted.FinishedAt != nil {
		t.Errorf("unresolved finalization was falsely made terminal: %+v", persisted)
	}
	// The failed attempt's operation breakdown must be retained with the
	// pending transition so a refreshed job detail still shows diagnostics
	// (audit #310); it is written atomically inside the pending transaction,
	// not leaked from the aborted 'succeeded' one.
	if len(persisted.Operations) != 1 || persisted.Operations[0].Target != "runtime" {
		t.Errorf("operation breakdown was not retained with the pending transition: %+v", persisted.Operations)
	}
}

func TestStartupFinalizesDurableRuntimePublicationReceipt(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"settings":{"panelListen":"127.0.0.1:2096"}}`)
	if err := NewSnapshotStore(db).Save(revision, payload); err != nil {
		t.Fatal(err)
	}
	digestBytes := sha256.Sum256(payload)
	digest := hex.EncodeToString(digestBytes[:])
	started := time.Now().Add(-time.Minute).Unix()
	job := Job{
		ID: "published-not-finalized", DesiredRevision: revision, BaseRevision: 0,
		Status: StatusApplying, Trigger: "mutation", ActorID: "dead-process",
		CreatedAt: started, StartedAt: &started,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	operations := []OperationResult{{Type: "promote", Target: "runtime", Success: true}}
	operationsJSON, err := json.Marshal(operations)
	if err != nil {
		t.Fatal(err)
	}
	// Plant the receipt exactly as a real publish left it: an explicit
	// 'published' phase, the converged disposition, the lease fencing fields,
	// and the matching phase-evidence row — not a bare legacy skeleton.
	publishedAt := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO runtime_publications
(job_id, revision, base_revision, generation, snapshot_sha256, operations_json,
 confirmations_json, published_at, owner_process, operation_id, lease_expires_at,
 phase, artifacts_json, service_phase, firewall_phase, updated_at, disposition)
VALUES(?, ?, 0, ?, ?, ?, '[]', ?, 'pid:1:dead', 'publish', 0,
 'published', '[]', 'converged', 'committed', ?, 'runtime_converged')`,
		job.ID, revision, 7, digest, string(operationsJSON), publishedAt, publishedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at)
VALUES(?, 'published', ?, ?, ?)`, job.ID, 7,
		`{"disposition":"runtime_converged"}`, publishedAt); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("startup recovery retried already-published runtime")
		return Result{}, nil
	})
	_ = runner
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != revision {
		t.Errorf("startup did not finalize published revision: %+v", state)
	}
	persisted, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusSucceeded || persisted.FinishedAt == nil {
		t.Errorf("published job was not finalized succeeded: %+v", persisted)
	}
	if len(persisted.Operations) != 1 || persisted.Operations[0].Target != "runtime" {
		t.Errorf("published operations were not recovered: %+v", persisted.Operations)
	}
	var receipts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Errorf("consumed runtime publication receipt remains: %d", receipts)
	}
	// The finalized receipt is archived, not just deleted: the history row
	// preserves the published phase and converged disposition as evidence.
	var finalPhase, receiptJSON, phasesJSON string
	if err := db.QueryRow(`SELECT final_phase,receipt_json,phases_json FROM runtime_publication_history WHERE job_id=?`, job.ID).
		Scan(&finalPhase, &receiptJSON, &phasesJSON); err != nil {
		t.Fatalf("finalized publication was not archived to history: %v", err)
	}
	if finalPhase != PublicationPhaseFinalized {
		t.Errorf("archived final phase = %q, want %q", finalPhase, PublicationPhaseFinalized)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(receiptJSON), &receipt); err != nil {
		t.Fatalf("archived receipt is not valid JSON: %v", err)
	}
	if receipt["phase"] != PublicationPhasePublished || receipt["disposition"] != string(ApplyDispositionRuntimeConverged) {
		t.Errorf("archived receipt lost publication evidence: %s", receiptJSON)
	}
	var phases []map[string]any
	if err := json.Unmarshal([]byte(phasesJSON), &phases); err != nil {
		t.Fatalf("archived phase history is not valid JSON: %v", err)
	}
	foundPublished := false
	for _, phase := range phases {
		if phase["phase"] == PublicationPhasePublished {
			foundPublished = true
		}
	}
	if !foundPublished {
		t.Errorf("archived phase history lacks the published evidence: %s", phasesJSON)
	}
}
