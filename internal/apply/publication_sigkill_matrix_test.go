package apply

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestApplyPublicationSIGKILLMatrix(t *testing.T) {
	if os.Getenv("VEIL_APPLY_SIGKILL_CHILD") == "1" {
		runApplyPublicationSIGKILLChild(t)
		return
	}
	boundaries := []struct {
		name            string
		expectApplied   bool
		expectJobStatus string
	}{
		{"before_first_artifact", false, StatusFailed},
		{"after_each_artifact", true, StatusSucceeded},
		{"after_artifact_manifest", true, StatusSucceeded},
		{"before_caddy_load", true, StatusSucceeded},
		{"after_caddy_load", true, StatusSucceeded},
		{"before_each_systemd_action", true, StatusSucceeded},
		{"after_each_systemd_action", true, StatusSucceeded},
		{"before_health", true, StatusSucceeded},
		{"after_health", true, StatusSucceeded},
		{"before_firewall_commit", true, StatusSucceeded},
		{"after_firewall_commit", true, StatusSucceeded},
		{"before_receipt_publication", true, StatusSucceeded},
		{"before_db_finalization", true, StatusSucceeded},
	}
	for _, boundary := range boundaries {
		boundary := boundary
		t.Run(boundary.name, func(t *testing.T) {
			dbPath := t.TempDir() + "/veil.db"
			db, err := storage.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := NewRevisionStore(db).BumpDesired()
			if err != nil {
				t.Fatal(err)
			}
			if err := NewSnapshotStore(db).Save(revision, []byte(`{"effectiveAt":1}`)); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestApplyPublicationSIGKILLMatrix$", "-test.v")
			cmd.Env = append(os.Environ(),
				"VEIL_APPLY_SIGKILL_CHILD=1",
				"VEIL_APPLY_SIGKILL_DB="+dbPath,
				"VEIL_APPLY_SIGKILL_BOUNDARY="+boundary.name,
			)
			err = cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ProcessState.ExitCode() >= 0 {
				t.Fatalf("child was not killed by SIGKILL: %v", err)
			}

			db, err = storage.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			runner := NewRunner(NewRevisionStore(db), NewJobStore(db), ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
				if err := markTestRuntimeConverged(ctx); err != nil {
					return Result{}, err
				}
				return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
			}))
			defer runner.Close()
			state, err := NewRevisionStore(db).Get()
			if err != nil {
				t.Fatal(err)
			}
			wantApplied := uint64(0)
			if boundary.expectApplied {
				wantApplied = revision
			}
			if state.Applied != wantApplied {
				t.Fatalf("applied revision at %s = %d, want %d", boundary.name, state.Applied, wantApplied)
			}
			jobs, err := NewJobStore(db).List(1)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("list crashed job: jobs=%+v err=%v", jobs, err)
			}
			if jobs[0].Status != boundary.expectJobStatus {
				t.Fatalf("job status at %s = %s, want %s", boundary.name, jobs[0].Status, boundary.expectJobStatus)
			}
		})
	}
}

func runApplyPublicationSIGKILLChild(t *testing.T) {
	db, err := storage.Open(os.Getenv("VEIL_APPLY_SIGKILL_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions := NewRevisionStore(db)
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	boundary := os.Getenv("VEIL_APPLY_SIGKILL_BOUNDARY")
	runner := NewRunner(revisions, NewJobStore(db), ContextExecutorFunc(func(ctx context.Context, revision uint64) (Result, error) {
		phases := phasesBeforeSIGKILLBoundary(boundary)
		for _, phase := range phases {
			if err := AdvanceRuntimePublication(ctx, phase, PublicationDetails{}); err != nil {
				return Result{}, err
			}
		}
		if boundary == "before_db_finalization" {
			fence, ok := FenceFromContext(ctx)
			if !ok {
				return Result{}, errors.New("missing fence")
			}
			var job Job
			job.DesiredRevision = revision
			if err := db.QueryRow(`SELECT id FROM apply_jobs WHERE owner_process=? AND lease_generation=? ORDER BY created_at DESC LIMIT 1`, fence.Owner, fence.Generation).Scan(&job.ID); err != nil {
				return Result{}, err
			}
			if err := recordRuntimePublication(db, job, fence.Generation, ApplyDispositionRuntimeConverged, nil, nil, false, time.Now().UTC().Unix()); err != nil {
				return Result{}, err
			}
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}))
	_, _ = runner.RunContext(context.Background(), state.Desired, "sigkill-matrix", "test")
	t.Fatal("child survived SIGKILL boundary")
}

func phasesBeforeSIGKILLBoundary(boundary string) []string {
	all := []string{
		PublicationPhaseArtifactsPrepared,
		PublicationPhaseArtifactsCommitted,
		PublicationPhaseServicesPlanned,
		PublicationPhaseServicesConverged,
		PublicationPhaseHealthVerified,
		PublicationPhaseFirewallCommitted,
	}
	count := map[string]int{
		"before_first_artifact":      0,
		"after_each_artifact":        1,
		"after_artifact_manifest":    2,
		"before_caddy_load":          3,
		"after_caddy_load":           4,
		"before_each_systemd_action": 3,
		"after_each_systemd_action":  4,
		"before_health":              4,
		"after_health":               5,
		"before_firewall_commit":     5,
		"after_firewall_commit":      6,
		"before_receipt_publication": 6,
		"before_db_finalization":     6,
	}[boundary]
	return all[:count]
}
