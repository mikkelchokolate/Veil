package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteUsernamePersistedRollsBackOnSaveFailure(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	deleted, err := registry.DeleteUsernamePersisted(session.Username)
	if deleted != 1 || err == nil {
		t.Fatalf("DeleteUsernamePersisted deleted=%d err=%v", deleted, err)
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed persistent user revocation removed the session")
	}
}

func TestDeleteAllExceptPersistedRollsBackOnSaveFailure(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	current, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	registry.path = blockedSessionStorePath(t)

	deleted, err := registry.DeleteAllExceptPersisted(current.Token)
	if deleted != 1 || err == nil {
		t.Fatalf("DeleteAllExceptPersisted deleted=%d err=%v", deleted, err)
	}
	if _, ok := registry.Get(current.Token); !ok {
		t.Fatal("current session was removed")
	}
	if _, ok := registry.Get(other.Token); !ok {
		t.Fatal("failed bulk revocation removed the other session")
	}
}

func blockedSessionStorePath(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "sessions.json")
}

func TestAtomicUserUpdateStopsWhenSessionPersistenceFails(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: session.Username, PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	req := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"admin","locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()

	state.handleAtomicUserUpdate(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errSessionRevocationPersistence.Error()) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "alice" && (user.Role != "viewer" || user.Locale != "en") {
			t.Fatalf("user changed despite revocation failure: %+v", user)
		}
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("session disappeared despite rollback")
	}
}

func TestBackupOwnerRevocationKeepsRetryTokenOnPersistenceFailure(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	state := &managementState{
		sessions:   registry,
		backupJobs: map[string]BackupRestoreJob{"job": {ID: "job", Status: "succeeded", ownerSessionToken: session.Token}},
	}

	state.revokeBackupRestoreOwnerSession("job", session.Token)

	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed backup owner revocation removed the session")
	}
	if got := state.backupJobs["job"].ownerSessionToken; got != session.Token {
		t.Fatalf("retry token was cleared: %q", got)
	}
}
