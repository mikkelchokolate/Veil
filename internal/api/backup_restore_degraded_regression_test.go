package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression for #964: when the helper commits a restore but the post-restore
// revalidation fails (degraded outcome), the restored session store is loaded
// — so every session except the restore owner's must still be revoked. Before
// the fix, revocation only ran on a fully clean success and a degraded restore
// left backup-era sessions authorized.
func TestBackupRestoreDegradedOutcomeStillRevokesSessions(t *testing.T) {
	stubManagementApplySideEffects(t)
	state := newPanelBackupState(t)
	// Force the post-restore reopen to leave the database unusable: the helper
	// commits the restored files (Restored=true) but ReloadLocked cannot reopen
	// veil.db, so the outcome degrades instead of succeeding.
	state.databaseOpener = func(string) (*sql.DB, error) {
		return nil, errors.New("injected post-restore database open failure")
	}
	for _, body := range []string{
		`{"username":"alice","password":"alice-password-123","role":"viewer"}`,
		`{"username":"bob","password":"bob-password-12345","role":"viewer"}`,
	} {
		request := adminJSONRequest(http.MethodPost, "/api/users", body)
		response := httptest.NewRecorder()
		state.handleUsersRoute(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("seed restored user status=%d body=%s", response.Code, response.Body.String())
		}
	}
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	ownerSession, err := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := state.sessionRegistry().Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}

	restore := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/restore", `{"confirm":true}`)
	restore.AddCookie(&http.Cookie{Name: "veil_session", Value: ownerSession.Token})
	restoreResponse := httptest.NewRecorder()
	state.handleBackupByName(restoreResponse, restore)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("restore status=%d body=%s", restoreResponse.Code, restoreResponse.Body.String())
	}
	var accepted BackupRestoreJob
	if err := json.NewDecoder(restoreResponse.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	var completed BackupRestoreJob
	for {
		job, ok := state.backupRestoreJob(accepted.ID)
		if !ok {
			t.Fatal("restore job disappeared")
		}
		if job.Status != "queued" && job.Status != "running" {
			completed = job
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore job timed out: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completed.Status != "degraded" || !completed.Restored || completed.Outcome != "restored" {
		t.Fatalf("expected degraded-but-restored job, got %+v", completed)
	}
	// The committed restore must have bulk-revoked every session except the
	// restore owner's — even though the outcome is degraded, not succeeded.
	if _, ok := state.sessionRegistry().Get(otherSession.Token); ok {
		t.Fatal("degraded restore left a restored backup-era session authorized")
	}
	if _, ok := state.sessionRegistry().Get(ownerSession.Token); !ok {
		t.Fatal("degraded restore revoked the owner session")
	}
}
