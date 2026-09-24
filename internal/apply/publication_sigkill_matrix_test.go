package apply

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

// TestApplyPublicationSIGKILLMatrix kills a child process mid-apply at each
// durable publication phase boundary and asserts the crash residual honestly:
//
//   - intent-only:      nothing mutated -> the job is finalized failed
//     (PUBLICATION_NOT_STARTED) and the receipt is consumed.
//   - mid-phase kills:  the durable receipt and per-phase evidence are
//     preserved and the job is transferred to recovery_pending — no
//     synthetic convergence is claimed on the crash alone.
//   - published:        the committed receipt survives the kill and recovery
//     finalizes it as succeeded+applied without re-running the executor.
//
// Each boundary name IS the phase the child durably recorded before SIGKILL,
// so no two boundaries share the same residual. Resume errors are fatal for
// every boundary that asserts a post-resume status.
func TestApplyPublicationSIGKILLMatrix(t *testing.T) {
	if os.Getenv("VEIL_APPLY_SIGKILL_CHILD") == "1" {
		runApplyPublicationSIGKILLChild(t)
		return
	}
	boundaries := []struct {
		name           string
		wantStatus     string
		wantErrorCode  string
		wantApplied    bool
		wantPending    bool   // durable receipt must be transferred to recovery at `name` phase
		resumeStatus   string // job status after an explicit recovery resume
		resumeExecutor bool   // resume must drive the executor exactly once
	}{
		{PublicationPhaseIntent, StatusFailed, "PUBLICATION_NOT_STARTED", false, false, StatusFailed, false},
		{PublicationPhaseArtifactsPrepared, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhaseArtifactsCommitted, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhaseServicesPlanned, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhaseServicesConverged, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhaseHealthVerified, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhaseFirewallCommitted, StatusRecoveryPending, "RECOVERY_PENDING", false, true, StatusSucceeded, true},
		{PublicationPhasePublished, StatusSucceeded, "", true, false, StatusSucceeded, false},
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
			var executorCalls atomic.Int32
			runner := NewRunner(NewRevisionStore(db), NewJobStore(db), ContextExecutorFunc(func(ctx context.Context, _ uint64) (Result, error) {
				executorCalls.Add(1)
				if err := markTestRuntimeConverged(ctx); err != nil {
					return Result{}, err
				}
				return Result{Success: true, Disposition: ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
			}))
			defer runner.Close()
			if err := runner.ReadinessError(); err != nil {
				t.Fatalf("recovery classification failed at %s: %v", boundary.name, err)
			}

			jobs, err := NewJobStore(db).List(1)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("list crashed job: jobs=%+v err=%v", jobs, err)
			}
			job := jobs[0]

			// Assert the durable crash residual honestly BEFORE any resume:
			// recovery classification alone must never run the executor or
			// claim runtime convergence it cannot prove.
			if executorCalls.Load() != 0 {
				t.Fatalf("recovery classification ran the executor %d times at %s", executorCalls.Load(), boundary.name)
			}
			if job.Status != boundary.wantStatus {
				t.Fatalf("job status at %s = %s, want %s", boundary.name, job.Status, boundary.wantStatus)
			}
			if job.ErrorCode != boundary.wantErrorCode {
				t.Fatalf("job error code at %s = %q, want %q", boundary.name, job.ErrorCode, boundary.wantErrorCode)
			}
			assertSIGKILLAppliedRevision(t, db, boundary.name, boundary.wantApplied, revision)
			assertSIGKILLCrashResidual(t, db, runner, job, boundary.name, boundary.wantPending)

			// Fail closed on resume errors for every boundary that claims a
			// status. Pending residuals resume through the real fenced retry
			// path — the executor re-runs the apply and converges durably —
			// while terminal residuals must not re-run the executor at all.
			if err := runner.resumeRecoveryPending(context.Background()); err != nil {
				t.Fatalf("resume crashed publication at %s: %v", boundary.name, err)
			}
			if boundary.resumeExecutor {
				if got := executorCalls.Load(); got != 1 {
					t.Fatalf("recovery resume ran the executor %d times at %s, want 1", got, boundary.name)
				}
			} else if got := executorCalls.Load(); got != 0 {
				t.Fatalf("executor ran %d times for terminal residual %s", got, boundary.name)
			}
			job, err = NewJobStore(db).Get(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status != boundary.resumeStatus {
				t.Fatalf("job status after resume at %s = %s, want %s", boundary.name, job.Status, boundary.resumeStatus)
			}
			wantAppliedAfterResume := boundary.wantApplied || boundary.resumeExecutor
			assertSIGKILLAppliedRevision(t, db, boundary.name, wantAppliedAfterResume, revision)
		})
	}
}

// assertSIGKILLAppliedRevision locks the durable applied revision for a
// boundary: a crash must never advance it unless a committed receipt existed.
func assertSIGKILLAppliedRevision(t *testing.T, db *sql.DB, boundary string, wantApplied bool, revision uint64) {
	t.Helper()
	state, err := NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(0)
	if wantApplied {
		want = revision
	}
	if state.Applied != want {
		t.Fatalf("applied revision at %s = %d, want %d", boundary, state.Applied, want)
	}
}

// assertSIGKILLCrashResidual verifies the durable publication journal left by
// the killed child: pending residuals keep their receipt (transferred to this
// runner's lease) and every committed phase's evidence; terminal residuals
// consume the receipt and archive the journal.
func assertSIGKILLCrashResidual(t *testing.T, db *sql.DB, runner *Runner, job Job, boundary string, wantPending bool) {
	t.Helper()
	wantPhases := append([]string{PublicationPhaseIntent}, phasesBeforeSIGKILLBoundary(t, boundary)...)
	if boundary == PublicationPhasePublished {
		wantPhases = append(wantPhases, PublicationPhasePublished)
	}
	var phase, owner string
	var generation uint64
	err := db.QueryRow(`SELECT phase,owner_process,generation FROM runtime_publications WHERE job_id=?`, job.ID).
		Scan(&phase, &owner, &generation)
	if wantPending {
		if err != nil {
			t.Fatalf("crash residual at %s lost its durable receipt: %v", boundary, err)
		}
		if phase != boundary {
			t.Fatalf("crash residual phase at %s = %q, want the killed boundary", boundary, phase)
		}
		if owner != runner.ownerID || generation == 0 || generation != job.LeaseGeneration {
			t.Fatalf("receipt at %s was not fenced to the recovery owner: owner=%q generation=%d job=%+v",
				boundary, owner, generation, job)
		}
		if job.Terminal() {
			t.Fatalf("recovery-pending residual at %s was finalized: %+v", boundary, job)
		}
		// The live per-phase evidence must equal exactly what the child durably
		// committed — this is what disambiguates the named kill points.
		if got := publicationPhaseEvidence(t, db, job.ID); !slices.Equal(got, wantPhases) {
			t.Fatalf("durable phase evidence at %s = %v, want %v", boundary, got, wantPhases)
		}
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("terminal residual at %s left a live receipt (phase=%q err=%v)", boundary, phase, err)
	}
	if !job.Terminal() {
		t.Fatalf("job at %s is not terminal: %+v", boundary, job)
	}
	var finalPhase, phasesJSON string
	if err := db.QueryRow(`SELECT final_phase,phases_json FROM runtime_publication_history WHERE job_id=?`, job.ID).
		Scan(&finalPhase, &phasesJSON); err != nil {
		t.Fatalf("terminal residual at %s kept no archived journal: %v", boundary, err)
	}
	wantFinal := PublicationPhaseRolledBack
	if boundary == PublicationPhasePublished {
		wantFinal = PublicationPhaseFinalized
	}
	if finalPhase != wantFinal {
		t.Fatalf("archived final phase at %s = %q, want %q", boundary, finalPhase, wantFinal)
	}
	// Finalization consumes the live receipt and archives its phase evidence
	// into the publication history journal.
	if got := publicationPhaseEvidence(t, db, job.ID); len(got) != 0 {
		t.Fatalf("terminal residual at %s left live phase evidence %v", boundary, got)
	}
	var archived []struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal([]byte(phasesJSON), &archived); err != nil {
		t.Fatalf("decode archived phases at %s: %v", boundary, err)
	}
	got := make([]string, 0, len(archived))
	for _, entry := range archived {
		got = append(got, entry.Phase)
	}
	slices.Sort(got)
	sortedWant := slices.Clone(wantPhases)
	slices.Sort(sortedWant)
	if !slices.Equal(got, sortedWant) {
		t.Fatalf("archived phase evidence at %s = %v, want %v", boundary, got, sortedWant)
	}
}

// publicationPhaseEvidence lists the durable per-phase journal rows in commit
// order for a job.
func publicationPhaseEvidence(t *testing.T, db *sql.DB, jobID string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT phase FROM runtime_publication_phases WHERE job_id=? ORDER BY rowid`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var phases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		phases = append(phases, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return phases
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
	phases := phasesBeforeSIGKILLBoundary(t, boundary)
	runner := NewRunner(revisions, NewJobStore(db), ContextExecutorFunc(func(ctx context.Context, revision uint64) (Result, error) {
		for _, phase := range phases {
			if err := AdvanceRuntimePublication(ctx, phase, PublicationDetails{}); err != nil {
				return Result{}, err
			}
		}
		if boundary == PublicationPhasePublished {
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

// phasesBeforeSIGKILLBoundary maps a boundary name to the publication phases
// the child durably commits before SIGKILL. The name is the last committed
// phase: each boundary therefore produces a distinct durable residual.
func phasesBeforeSIGKILLBoundary(t *testing.T, boundary string) []string {
	t.Helper()
	all := []string{
		PublicationPhaseArtifactsPrepared,
		PublicationPhaseArtifactsCommitted,
		PublicationPhaseServicesPlanned,
		PublicationPhaseServicesConverged,
		PublicationPhaseHealthVerified,
		PublicationPhaseFirewallCommitted,
	}
	switch boundary {
	case PublicationPhaseIntent:
		return nil
	case PublicationPhasePublished:
		return all
	}
	index := slices.Index(all, boundary)
	if index < 0 {
		t.Fatalf("unknown SIGKILL boundary %q", boundary)
	}
	return all[:index+1]
}
