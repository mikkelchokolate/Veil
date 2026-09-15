package apply

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicationRecoveryDoesNotSpamFailedJobsForOneRevision(t *testing.T) {
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

	var attempts atomic.Int32
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
		attempts.Add(1)
		if err := MarkRuntimeMutationStarting(ctx, PublicationDetails{
			ExpectedLiveManifestSHA256: "expected",
			PreviousLiveManifestSHA256: "previous",
			Artifacts:                  []string{"config.json"},
			LiveRoot:                   t.TempDir(),
		}); err != nil {
			return Result{}, err
		}
		if err := AdvanceRuntimePublication(ctx, PublicationPhaseArtifactsCommitted, PublicationDetails{
			FirewallPhase: "prepare-failed",
		}); err != nil {
			return Result{}, err
		}
		return Result{
			Success:      false,
			ErrorCode:    "FIREWALL_PREPARE_FAILED",
			ErrorMessage: "ufw prepare failed: invalid syntax",
			RuntimeMutation: RuntimeMutationOutcome{
				MutationStarted:  true,
				ArtifactsChanged: true,
				FirewallChanged:  true,
				RollbackComplete: true,
			},
		}, errors.New("ufw prepare failed: invalid syntax")
	}))
	defer runner.Close()

	if _, err := runner.RunContext(context.Background(), revision, "mutation", "admin"); err == nil {
		t.Fatal("expected firewall prepare failure")
	}

	time.Sleep(1200 * time.Millisecond)
	listed, err := jobs.List(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("recovery created %d jobs for one revision in a few seconds: %+v", len(listed), listed)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("recovery tight-looped ApplyLive+ApplyServices %d times in a few seconds", got)
	}
	assertRetainedFirewallPrepareError(t, listed[0])

	runner.mu.Lock()
	runner.lastRecoveryAttempt = time.Time{}
	runner.mu.Unlock()
	if err := runner.resumeRecoveryPending(context.Background()); err == nil {
		t.Fatal("expected in-place recovery to surface the firewall prepare failure")
	}
	if attempts.Load() < 2 {
		t.Fatal("recovery never retried the same job after backoff")
	}
	listed, err = jobs.List(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("in-place recovery created extra jobs: %+v", listed)
	}
	assertRetainedFirewallPrepareError(t, listed[0])
}

func TestRecoveryPendingDueUsesExistingTimestamps(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	now := time.Now().Unix()
	due, err := jobs.RecoveryPendingDue(now)
	if err != nil {
		t.Fatalf("RecoveryPendingDue on empty store: %v", err)
	}
	if due {
		t.Fatal("empty store reported recovery due")
	}

	started := now - 30
	job := Job{
		ID: "rp-due", DesiredRevision: 1, Status: StatusRecoveryPending,
		Trigger: "mutation", CreatedAt: now - 60, StartedAt: &started,
	}
	if err := jobs.Create(job); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE apply_jobs SET started_at=? WHERE id=?`, started, job.ID); err != nil {
		t.Fatal(err)
	}
	due, err = jobs.RecoveryPendingDue(now - 10)
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("recovery job older than cutoff was not due")
	}
	due, err = jobs.RecoveryPendingDue(now - 40)
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("recovery job newer than cutoff was due")
	}
}

func assertRetainedFirewallPrepareError(t *testing.T, job Job) {
	t.Helper()
	if job.ErrorCode == "PUBLICATION_RECOVERY_TRANSFERRED" || strings.Contains(job.ErrorMessage, "transferred to a fresh full-convergence") {
		t.Fatalf("original firewall error was overwritten: %+v", job)
	}
	if job.ErrorCode != "FIREWALL_PREPARE_FAILED" || !strings.Contains(job.ErrorMessage, "ufw prepare failed") {
		t.Fatalf("original firewall error was not retained: %+v", job)
	}
	if job.Trigger == "publication-recovery" {
		t.Fatalf("recovery created a transferred job row: %+v", job)
	}
}
