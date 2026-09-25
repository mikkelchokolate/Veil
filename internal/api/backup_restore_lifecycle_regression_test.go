package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// seedRestoreJob registers a queued restore job exactly the way
// queuePanelBackupRestore does before handing off to runPanelBackupRestore.
func seedRestoreJob(t *testing.T, state *managementState, jobID, archive string) {
	t.Helper()
	state.backupJobsMu.Lock()
	state.backupJobs[jobID] = BackupRestoreJob{ID: jobID, Archive: archive, Status: "queued", CreatedAt: time.Now().UTC()}
	if err := state.persistBackupRestoreJobsLocked(); err != nil {
		state.backupJobsMu.Unlock()
		t.Fatal(err)
	}
	state.backupJobsMu.Unlock()
}

func startTestRestore(t *testing.T, state *managementState, jobID, archive string) (done <-chan struct{}) {
	t.Helper()
	// The HTTP handler owns backupMutationMu through beginBackupMutation;
	// callers driving runPanelBackupRestore directly must hold it the same
	// way — the runner releases it when it returns.
	state.backupMutationMu.Lock()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		state.runPanelBackupRestore(jobID, archive, "", "admin", "admin", "127.0.0.1", "test")
	}()
	return finished
}

// TestRestoreReloadDefersRecoveryPendingResume covers #1067: a restored
// veil.db can contain a recovery_pending apply job, and the reload inside the
// restore (which holds s.mu) must not synchronously resume it — the resume
// invokes the apply executor, which locks s.mu itself and deadlocked the
// whole panel. The deferred runner leaves the resume to its monitor.
func TestRestoreReloadDefersRecoveryPendingResume(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	// A recovery-pending job for a revision that is already applied: the
	// non-deferred constructor path resolves it to SUPERSEDED/failed inline,
	// which is the synchronous recovery pass this test must observe NOT
	// running during a mutex-held reload.
	jobID := "restore-recovery-" + uuid.NewString()
	if err := state.applyJobs.Create(veilapply.Job{
		ID: jobID, DesiredRevision: revisions.Applied, BaseRevision: revisions.Applied,
		Status: veilapply.StatusRecoveryPending, Trigger: "retry", ActorID: "system",
		CreatedAt: time.Now().Unix(), OwnerProcess: "pid:1:stale", LeaseGeneration: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`INSERT INTO runtime_publications(job_id, revision, generation, snapshot_sha256, operations_json, published_at, phase)
VALUES(?,?,?,?,?,?,?)`, jobID, revisions.Applied, 1, "", "[]", time.Now().Unix(), "services_planned"); err != nil {
		t.Fatal(err)
	}

	// Mirror the restore sequence — the database is swapped out (closed here),
	// then the reload runs under s.mu — but in a goroutine with a bound so a
	// regression reports a deadlock instead of hanging the whole suite: the
	// pre-fix path ran the recovery resume inside the constructor, whose
	// executor re-enters the s.mu this goroutine is holding.
	reloaded := make(chan error, 1)
	go func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		if err := closeClientDatabase(state); err != nil {
			reloaded <- err
			return
		}
		reloaded <- NewManagementStateLifecycle(state).ReloadLocked()
	}()
	select {
	case err := <-reloaded:
		if err != nil {
			t.Fatalf("reload after database swap: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("post-restore reload deadlocked: runner construction synchronously resumed a recovery job whose executor re-enters s.mu (#1067)")
	}
	state.mu.Lock()
	jobs, runner := state.applyJobs, state.applyRunner
	state.mu.Unlock()
	if jobs == nil || runner == nil {
		t.Fatal("reload did not rebuild the apply subsystem")
	}
	persisted, err := jobs.Get(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != veilapply.StatusRecoveryPending {
		t.Fatalf("reload synchronously resolved recovery job to %q — the inline resume re-entered the apply executor while s.mu was held", persisted.Status)
	}
	if err := runner.ReadinessError(); err != nil {
		t.Fatalf("runner readiness after deferred construction: %v", err)
	}
}

// TestBackupRestoreReplacesStaleInMemoryState covers #1053: fields the backup
// does not carry must not survive the post-restore reload. ApplySnapshot
// merges (skips empty fields), so the restore rewinds mutable state to the
// serve-time defaults first — the restored snapshot then replaces it exactly
// like a cold start.
func TestBackupRestoreReplacesStaleInMemoryState(t *testing.T) {
	stubManagementApplySideEffects(t)
	state := newPanelBackupState(t)

	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created BackupCreateResponse
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// Mutate fields the backup snapshot does not carry: empty/absent fields
	// in the restored snapshot previously merged over these stale values.
	state.mu.Lock()
	state.routingPreset = "stale-preset"
	state.settings.Domain = "stale.example.com"
	state.routingSource = RoutingSource{Repository: "stale-repo"}
	state.users = append(state.users, User{Username: "ghost", PasswordHash: "hash", Role: "viewer"})
	state.mu.Unlock()

	restore := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/restore", `{"confirm":true}`)
	restoreResponse := httptest.NewRecorder()
	state.handleBackupByName(restoreResponse, restore)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("restore status=%d body=%s", restoreResponse.Code, restoreResponse.Body.String())
	}
	var accepted BackupRestoreJob
	if err := json.Unmarshal(restoreResponse.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	// The restore does real privileged-helper work plus a post-restore apply
	// convergence — give it headroom: the convergence runner itself is
	// test-bound at 15s and a slow environment legitimately lands the job at
	// "degraded" (revalidation_failed on the convergence timeout), which is
	// not the behavior under test.
	deadline := time.Now().Add(90 * time.Second)
	var job BackupRestoreJob
	for {
		var ok bool
		job, ok = state.backupRestoreJob(accepted.ID)
		if !ok {
			t.Fatal("restore job disappeared")
		}
		if job.Status == "succeeded" || job.Status == "degraded" || job.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore job timed out: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status == "failed" {
		t.Fatalf("restore job failed: %+v", job)
	}
	// "degraded" is only acceptable when the reload itself worked and the
	// convergence timed out: a failed reopen leaves s.db nil.
	state.mu.Lock()
	reopened := state.db != nil && state.applyRunner != nil
	state.mu.Unlock()
	if !reopened {
		t.Fatalf("restore did not reopen the database/apply subsystem: %+v", job)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	// Positive control: the reload must have applied the restored snapshot —
	// panelListen comes from the backup's state file, proving these
	// assertions exercise the apply path rather than an early-return reset.
	if state.settings.PanelListen == "" {
		t.Fatalf("restored snapshot was not applied: settings.PanelListen empty, job=%+v", job)
	}
	if state.routingPreset != "" {
		t.Fatalf("pre-restore routingPreset %q survived restore — reload merged instead of replacing", state.routingPreset)
	}
	if state.settings.Domain != "" {
		t.Fatalf("pre-restore settings.Domain %q survived restore", state.settings.Domain)
	}
	if state.routingSource.Repository != "" {
		t.Fatalf("pre-restore routingSource %q survived restore", state.routingSource.Repository)
	}
	for _, user := range state.users {
		if user.Username == "ghost" {
			t.Fatal("pre-restore user survived restore — stale in-memory state was not replaced")
		}
	}
}

// TestRestoreJoinsPanelUpdateGoroutine covers #1068: the panel-update restart
// goroutine is tracked by updateWG and reads s.applyRunner/s.db without s.mu.
// The restore must join it before closing the database, otherwise the
// goroutine races the swap and can dereference a nil runner.
func TestRestoreJoinsPanelUpdateGoroutine(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	client := &blockingRestorePrivilegedClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	state.privileged = client
	state.privilegedLocal = false

	releaseUpdate := make(chan struct{})
	var releaseUpdateOnce sync.Once
	finishUpdate := func() { releaseUpdateOnce.Do(func() { close(releaseUpdate) }) }
	state.updateWG.Add(1)
	go func() {
		defer state.updateWG.Done()
		<-releaseUpdate
	}()

	seedRestoreJob(t, state, "restore-updatewg", "test.enc")
	done := startTestRestore(t, state, "restore-updatewg", "test.enc")
	defer func() {
		finishUpdate()
		close(client.release)
		<-done
	}()

	select {
	case <-client.started:
		t.Fatal("restore invoked the privileged helper while a panel-update goroutine was still in flight")
	case <-time.After(3 * time.Second):
	}
	finishUpdate()
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not proceed after the panel-update goroutine finished")
	}
}

// cancelAwareRestoreClient blocks inside the helper restore call and records
// whether it was released normally or aborted by the lifecycle context —
// the exact #1069 failure mode (Close canceled an in-flight restore).
type cancelAwareRestoreClient struct {
	*recordingPrivilegedClient
	started   chan struct{}
	release   chan struct{}
	viaCancel atomic.Bool
	once      sync.Once
}

func (c *cancelAwareRestoreClient) Backup(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	if request.Action != privileged.BackupActionRestore {
		return c.recordingPrivilegedClient.Backup(ctx, request)
	}
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return privileged.BackupResult{Restored: false}, errors.New("injected restore stop")
	case <-ctx.Done():
		c.viaCancel.Store(true)
		return privileged.BackupResult{}, ctx.Err()
	}
}

// TestCloseWaitsForInFlightBackupRestore covers #1069: Close must not cancel
// the lifecycle context (or read s.applyRunner) while a restore is mid-flight
// — runPanelBackupRestore holds clientRequestMu for its whole duration, so
// Close must take that write gate first. Before the fix lifecycleCancel ran
// unconditionally and aborted the helper's context mid-restore.
func TestCloseWaitsForInFlightBackupRestore(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	client := &cancelAwareRestoreClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	state.privileged = client
	state.privilegedLocal = false

	seedRestoreJob(t, state, "restore-close-race", "test.enc")
	restoreDone := startTestRestore(t, state, "restore-close-race", "test.enc")
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("restore helper did not start")
	}

	closed := make(chan error, 1)
	go func() { closed <- state.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while a restore was still in flight: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(client.release)
	select {
	case <-restoreDone:
	case <-time.After(10 * time.Second):
		t.Fatal("restore did not finish after the helper was released")
	}
	if client.viaCancel.Load() {
		t.Fatal("Close canceled the lifecycle context under an in-flight restore — the helper's context was aborted mid-restore")
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close wedged after the in-flight restore completed")
	}
}

// TestRestoreReleasesFencingLeaseAfterFailedReopen covers #996: when the
// post-restore reload cannot reopen veil.db, s.db stays nil — but the lease
// row still lives in the database file on disk and must be floored/released
// anyway, or it blocks every fenced operation until the two-hour TTL elapses.
func TestRestoreReleasesFencingLeaseAfterFailedReopen(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	databasePath := filepath.Join(filepath.Dir(state.statePath), "veil.db")
	// Fail only the opener call made from initApplySubsystem (matched on the
	// caller's stack frame, so the injection is robust to changes in how many
	// opens the reload performs before it): the reload then leaves s.db nil
	// while the on-disk database still holds the live restore lease.
	state.databaseOpener = func(path string) (*sql.DB, error) {
		pcs := make([]uintptr, 64)
		frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs)])
		for {
			frame, more := frames.Next()
			if strings.Contains(frame.Function, "initApplySubsystem") {
				return nil, errors.New("injected reopen failure")
			}
			if !more {
				break
			}
		}
		return storage.Open(path)
	}

	seedRestoreJob(t, state, "restore-fence-release", "test.enc")
	done := startTestRestore(t, state, "restore-fence-release", "test.enc")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("restore did not finish")
	}

	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatalf("open database after restore: %v", err)
	}
	defer db.Close()
	lease, err := veilapply.NewLeaseStore(db).Current()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Owner != "" || lease.ExpiresAt != 0 {
		t.Fatalf("restore fencing lease still held after failed reopen: %+v", lease)
	}
}
