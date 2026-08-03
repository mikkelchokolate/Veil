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
	if persisted.Status != StatusApplying || persisted.ErrorCode != "FINALIZATION_PENDING" {
		t.Errorf("unresolved finalization was not durably retained: %+v", persisted)
	}
	if persisted.FinishedAt != nil {
		t.Errorf("unresolved finalization was falsely made terminal: %+v", persisted)
	}
	if len(persisted.Operations) != 0 {
		t.Errorf("operations committed outside failed finalization transaction: %+v", persisted.Operations)
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
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS runtime_publications (
 job_id TEXT PRIMARY KEY,
 revision INTEGER NOT NULL,
 generation INTEGER NOT NULL,
 snapshot_sha256 TEXT NOT NULL,
 operations_json TEXT NOT NULL,
 published_at INTEGER NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at)
VALUES(?, ?, ?, ?, ?, ?)`, job.ID, revision, 7, digest, string(operationsJSON), time.Now().Unix()); err != nil {
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
}
