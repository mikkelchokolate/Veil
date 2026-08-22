package apply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunnerPersistsPublicationIntentBeforeExecutorMutation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSnapshotStore(db).Save(revision, []byte(`{"effectiveAt":1}`)); err != nil {
		t.Fatal(err)
	}

	var observed bool
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, gotRevision uint64) (Result, error) {
		fence, ok := FenceFromContext(ctx)
		if !ok {
			t.Fatal("executor did not receive a fence")
		}
		var persistedRevision, generation uint64
		var owner, operationID, phase, snapshotDigest string
		var leaseExpiresAt int64
		err := db.QueryRow(`SELECT revision,generation,owner_process,operation_id,lease_expires_at,phase,snapshot_sha256
FROM runtime_publications WHERE revision=?`, gotRevision).
			Scan(&persistedRevision, &generation, &owner, &operationID, &leaseExpiresAt, &phase, &snapshotDigest)
		if err != nil {
			return Result{}, errors.New("publication intent was not durable before executor mutation: " + err.Error())
		}
		if persistedRevision != gotRevision || generation != fence.Generation || owner != fence.Owner || operationID == "" ||
			leaseExpiresAt <= time.Now().Unix() || phase != "intent" || len(snapshotDigest) != 64 {
			return Result{}, errors.New("incomplete publication intent before executor mutation")
		}
		observed = true
		if err := markTestRuntimeConverged(ctx); err != nil {
			return Result{}, err
		}
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}))
	defer runner.Close()
	if _, err := runner.RunContext(context.Background(), revision, "manual", "actor"); err != nil {
		t.Fatalf("run with durable publication intent: %v", err)
	}
	if !observed {
		t.Fatal("executor did not observe pre-publication intent")
	}
}

func TestFinalizationFailureRetainsLeaseAndBlocksNewerApply(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSnapshotStore(db).Save(revision, []byte(`{"effectiveAt":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER fail_finalization_terminal_write
BEFORE UPDATE OF status ON apply_jobs
WHEN NEW.status = 'succeeded'
BEGIN
  SELECT RAISE(ABORT, 'injected finalization write failure');
END;`); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		if err := markTestRuntimeConverged(ctx); err != nil {
			return Result{}, err
		}
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true, Operations: []OperationResult{{Type: "promote", Target: "runtime", Success: true}}}, nil
	}))
	defer runner.Close()
	job, runErr := runner.RunContext(context.Background(), revision, "mutation", "actor")
	if runErr == nil || !strings.Contains(runErr.Error(), "injected finalization") {
		t.Fatalf("injected finalization failure was not surfaced: %v", runErr)
	}
	lease, err := NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner == "" || lease.Generation != job.LeaseGeneration || lease.Operation == "" || lease.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("unresolved publication released or corrupted its lease: lease=%+v job=%+v", lease, job)
	}
	var publications int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&publications); err != nil {
		t.Fatal(err)
	}
	if publications != 1 {
		t.Fatalf("unresolved publication receipt count=%d, want 1", publications)
	}
	second := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		t.Fatal("newer apply executed while older publication finalization was unresolved")
		return Result{}, nil
	})
	defer second.Close()
	if _, err := second.Run(revision, "retry", "actor"); !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("newer apply error=%v, want ErrApplyBusy", err)
	}
}

func TestRunnerRecoversStaleJobBeforeEveryAcquisition(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	revision, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSnapshotStore(db).Save(revision, []byte(`{"effectiveAt":1}`)); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	defer runner.Close()
	started := time.Now().Add(-time.Minute).Unix()
	stale := Job{
		ID: "created-after-runner-startup", DesiredRevision: revision, BaseRevision: 0,
		Status: StatusApplying, Trigger: "mutation", ActorID: "dead-owner",
		CreatedAt: started, StartedAt: &started, OwnerProcess: "pid:999999:stale", LeaseGeneration: 1,
	}
	if err := jobs.Create(stale); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(revision, "manual", "actor"); err != nil {
		t.Fatalf("new apply after stale recovery: %v", err)
	}
	recovered, err := jobs.Get(stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusFailed || recovered.ErrorCode != "INTERRUPTED" || recovered.FinishedAt == nil {
		t.Fatalf("stale job created after Runner construction was not recovered before acquisition: %+v", recovered)
	}
}
