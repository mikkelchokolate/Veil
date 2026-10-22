package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #1266/#1268: the succeeded restore owner's session is ended by a timed
// grace, not by reading the job, and the grace constant is actually wired.

func seedSucceededRestoreJob(t *testing.T, state *managementState, id string) Session {
	t.Helper()
	request := adminJSONRequest(http.MethodPost, "/api/users", `{"username":"alice","password":"alice-password-123","role":"admin"}`)
	response := httptest.NewRecorder()
	state.handleUsersRoute(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("seed owner user status=%d body=%s", response.Code, response.Body.String())
	}
	owner, err := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	state.backupJobsMu.Lock()
	state.backupJobs[id] = BackupRestoreJob{ID: id, Status: "succeeded", CreatedAt: time.Now().UTC(), ownerSessionToken: owner.Token}
	state.backupJobsMu.Unlock()
	return owner
}

func ownerSessionLive(state *managementState, token string) bool {
	_, ok := state.sessionRegistry().Get(token)
	return ok
}

func TestBackupRestoreOwnerSessionRevokedAfterGrace(t *testing.T) {
	state := newPanelBackupState(t)
	state.backupRestoreOwnerSessionGrace = 150 * time.Millisecond
	owner := seedSucceededRestoreJob(t, state, "job-grace")

	state.scheduleBackupRestoreOwnerSessionRevocation("job-grace", owner.Token)
	if !ownerSessionLive(state, owner.Token) {
		t.Fatal("owner valid in restored state lost its session before the grace elapsed")
	}
	deadline := time.Now().Add(10 * time.Second)
	for ownerSessionLive(state, owner.Token) {
		if time.Now().After(deadline) {
			t.Fatal("owner session survived past the grace: revocation never fired")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job, _ := state.backupRestoreJob("job-grace"); job.ownerSessionToken != "" {
		t.Fatalf("job kept the revoked owner token: %+v", job)
	}
	state.backupJobsMu.Lock()
	pending := len(state.backupOwnerRevocations)
	state.backupJobsMu.Unlock()
	if pending != 0 {
		t.Fatalf("fired grace timer left %d pending entries", pending)
	}
}

func TestBackupRestoreOwnerSessionNegativeGraceRevokesImmediately(t *testing.T) {
	state := newPanelBackupState(t)
	state.backupRestoreOwnerSessionGrace = -1
	owner := seedSucceededRestoreJob(t, state, "job-now")
	state.scheduleBackupRestoreOwnerSessionRevocation("job-now", owner.Token)
	if ownerSessionLive(state, owner.Token) {
		t.Fatal("negative grace must revoke the owner session synchronously")
	}
}

func TestBackupRestoreOwnerSessionDefaultGraceIsWired(t *testing.T) {
	state := newPanelBackupState(t)
	if got := state.backupRestoreOwnerSessionGraceDuration(); got != defaultBackupRestoreOwnerSessionGrace {
		t.Fatalf("unset grace = %v, want default %v", got, defaultBackupRestoreOwnerSessionGrace)
	}
	owner := seedSucceededRestoreJob(t, state, "job-default")
	state.scheduleBackupRestoreOwnerSessionRevocation("job-default", owner.Token)
	if !ownerSessionLive(state, owner.Token) {
		t.Fatal("default grace revoked the owner session immediately")
	}
	state.backupJobsMu.Lock()
	_, armed := state.backupOwnerRevocations["job-default"]
	state.backupJobsMu.Unlock()
	if !armed {
		t.Fatal("default grace did not arm a revocation timer")
	}
	// Shutdown cannot carry the grace across a restart, so it flushes.
	state.flushBackupRestoreOwnerSessionRevocations()
	if ownerSessionLive(state, owner.Token) {
		t.Fatal("flush left the pending owner session alive")
	}
}

func TestBackupRestoreCloseFlushesPendingOwnerGrace(t *testing.T) {
	state := newPanelBackupState(t)
	state.backupRestoreOwnerSessionGrace = time.Hour
	owner := seedSucceededRestoreJob(t, state, "job-close")
	state.scheduleBackupRestoreOwnerSessionRevocation("job-close", owner.Token)
	if !ownerSessionLive(state, owner.Token) {
		t.Fatal("precondition: owner session should be in its grace")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if ownerSessionLive(state, owner.Token) {
		t.Fatal("Close left a pending-grace owner session alive to natural expiry")
	}
}
