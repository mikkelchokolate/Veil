package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// overwritingRestoreClient simulates the privileged helper committing a
// restored state file whose digest the live veil.db never bound to its desired
// revision — the degraded shape from #1084: the helper reports Restored=true,
// then the post-restore ReloadLocked fails verifyCurrentStateRevisionLocked
// while s.db reopened cleanly (so a nil database cannot gate mutations).
type overwritingRestoreClient struct {
	*recordingPrivilegedClient
	state *managementState
}

func (c *overwritingRestoreClient) Backup(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	if request.Action != privileged.BackupActionRestore {
		return c.recordingPrivilegedClient.Backup(ctx, request)
	}
	c.state.mu.Lock()
	store := managementstate.NewStore(c.state.statePath, c.state.cipher)
	c.state.mu.Unlock()
	snapshot, ok, err := store.Load()
	if err != nil {
		return privileged.BackupResult{}, err
	}
	if !ok {
		return privileged.BackupResult{}, errors.New("test fixture: state file missing")
	}
	snapshot.Settings.Domain = "restored-unbound.example"
	if err := store.Save(snapshot); err != nil {
		return privileged.BackupResult{}, err
	}
	return privileged.BackupResult{ArchiveName: request.ArchiveName, Verified: true, Restored: true}, nil
}

// TestRestoreFailedReloadFailsClosedAgainstMutations covers #1084: when the
// post-restore reload fails after resetMutableStateToDefaultsLocked, the
// in-memory state holds serve-time defaults (0 users, setup incomplete). The
// restore must fail closed exactly like Reload() — startupStateLoadFailed
// blocks every mutation path — otherwise the next mutation persists those
// defaults over the committed restored state.json and wipes every user.
func TestRestoreFailedReloadFailsClosedAgainstMutations(t *testing.T) {
	stubManagementApplySideEffects(t)
	state := newPanelBackupState(t)

	// Seed a real user so a defaults-persist regression would visibly wipe it.
	seed := adminJSONRequest(http.MethodPost, "/api/users", `{"username":"operator","password":"operator-password-123","role":"viewer"}`)
	seedResponse := httptest.NewRecorder()
	state.handleUsersRoute(seedResponse, seed)
	if seedResponse.Code != http.StatusCreated {
		t.Fatalf("seed user status=%d body=%s", seedResponse.Code, seedResponse.Body.String())
	}
	preRestore, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}

	state.privileged = &overwritingRestoreClient{recordingPrivilegedClient: &recordingPrivilegedClient{}, state: state}
	state.privilegedLocal = false

	seedRestoreJob(t, state, "restore-fail-closed", "test.enc")
	done := startTestRestore(t, state, "restore-fail-closed", "test.enc")
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("restore did not finish")
	}

	job, ok := state.backupRestoreJob("restore-fail-closed")
	if !ok {
		t.Fatal("restore job missing")
	}
	if job.Status != "degraded" || !job.Restored || job.Phase != "revalidation_failed" {
		t.Fatalf("expected degraded/restored/revalidation_failed job, got %+v", job)
	}

	// The helper's committed state file is on disk (different bytes than the
	// pre-restore file) — the reload failed against it, not against nothing.
	restoredBody, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(restoredBody, preRestore) {
		t.Fatal("test fixture: the helper restore never replaced state.json")
	}

	state.mu.Lock()
	loadFailed := state.startupStateLoadFailed
	loadErr := state.startupStateLoadErr
	dbOpen := state.db != nil
	devAnonymous := state.allowDevAnonymous
	inMemoryUsers := len(state.users)
	setupCompleted := state.setup.Completed
	state.mu.Unlock()
	// The pre-reload reset did run — serve-time defaults (0 users, setup
	// incomplete) are exactly what a mutation would have persisted.
	if inMemoryUsers != 0 || setupCompleted {
		t.Fatalf("test fixture did not reach the reset state: users=%d setup.Completed=%v", inMemoryUsers, setupCompleted)
	}
	if !loadFailed || loadErr == nil {
		t.Fatalf("failed post-restore reload did not fail closed: startupStateLoadFailed=%v err=%v", loadFailed, loadErr)
	}
	if !strings.Contains(loadErr.Error(), "verify state revision") {
		t.Fatalf("expected revision verification failure, got %v", loadErr)
	}
	if !dbOpen {
		t.Fatal("test fixture lost its database handle — the regression needs s.db open so only startupStateLoadFailed gates mutations")
	}
	if devAnonymous {
		t.Fatal("failed reload left allowDevAnonymous enabled")
	}

	// The dangerous part: an ordinary mutation must be refused 503 and must
	// leave the committed restored file byte-identical. Before the fix the
	// mutation persisted the in-memory defaults (0 users) over it.
	mutation := adminJSONRequest(http.MethodPost, "/api/users", `{"username":"late-admin","password":"late-admin-password","role":"admin"}`)
	gate := degradedStateMiddleware(state, http.HandlerFunc(state.handleUsersRoute))
	mutationResponse := httptest.NewRecorder()
	gate.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("mutation after failed post-restore reload status=%d body=%s, want 503", mutationResponse.Code, mutationResponse.Body.String())
	}
	after, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, restoredBody) {
		t.Fatal("mutation persisted in-memory defaults over the restored state.json — restored users wiped")
	}
}

// TestRestoreClosesApplyRunnerAfterMutexRelease covers #1087: runner.Close
// waits on the recovery monitor, and the monitor re-enters s.mu through the
// apply executor. The restore therefore detaches the runner under s.mu and
// only runs the blocking Close after the mutex is released — closing under
// s.mu deadlocked restore whenever a recovery-pending job was due.
func TestRestoreClosesApplyRunnerAfterMutexRelease(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.mu.Lock()
	if state.applyRunner == nil {
		state.mu.Unlock()
		t.Fatal("apply-tracked fixture has no runner")
	}
	state.mu.Unlock()

	var closedUnderMutex atomic.Bool
	var closeCalls atomic.Int32
	original := applyRunnerClose
	applyRunnerClose = func(runner *veilapply.Runner) {
		closeCalls.Add(1)
		// TryLock fails exactly when this goroutine (or anyone) still holds
		// s.mu — the restore holds it across the whole pre-fix close.
		if state.mu.TryLock() {
			state.mu.Unlock()
		} else {
			closedUnderMutex.Store(true)
		}
		original(runner)
	}
	t.Cleanup(func() { applyRunnerClose = original })

	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	seedRestoreJob(t, state, "restore-runner-close", "test.enc")
	done := startTestRestore(t, state, "restore-runner-close", "test.enc")
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("restore did not finish — possible runner.Close deadlock")
	}
	if closeCalls.Load() == 0 {
		t.Fatal("restore never closed the detached apply runner")
	}
	if closedUnderMutex.Load() {
		t.Fatal("apply runner Close ran while s.mu was still held — the recovery monitor can deadlock restore (#1087)")
	}

	// The same invariant applies to managementState.Close: detach under s.mu,
	// join the runner monitor after the mutex is released.
	if err := state.Close(); err != nil {
		t.Fatalf("state close: %v", err)
	}
	if closedUnderMutex.Load() {
		t.Fatal("managementState.Close closed the apply runner while holding s.mu")
	}
}
