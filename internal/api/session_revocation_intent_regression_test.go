package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression for #1059: the credential mutation commits to state.json before
// the delete_many journal record is written, so a crash in that window would
// resurrect sessions minted under the old credentials at the next load. The
// handler therefore journals a revoke_username intent BEFORE the mutation
// commits; these tests pin the intent's replay semantics.

func TestRevocationIntentRevokesSessionsOnReloadAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	// Journal the intent as the mutation does, then stop: no delete_many —
	// this is exactly the crash window between the state commit and the
	// session revocation journal record.
	if _, err := registry.MarkUsernameRevocationPending("alice"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(alice.Token); ok {
		t.Fatal("old-credential session survived a crash before delete_many")
	}
	if _, ok := reloaded.Get(bob.Token); !ok {
		t.Fatal("revocation intent deleted an unrelated user's session")
	}
}

func TestRevocationIntentDoesNotRevokeNewerSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	// The fake clock stays near the real wall clock so reloaded sessions are
	// still inside their idle/absolute lifetimes; only the ordering matters.
	base := time.Now().UTC()
	registry.now = func() time.Time { return base }
	oldSession, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return base.Add(time.Second) }
	if _, err := registry.MarkUsernameRevocationPending("alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.DeleteUsernamePersisted("alice"); err != nil {
		t.Fatal(err)
	}
	// A re-login after the mutation must not be clobbered when the stale
	// revoke_username record replays on the next process start.
	registry.now = func() time.Time { return base.Add(2 * time.Second) }
	newSession, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(newSession.Token); !ok {
		t.Fatal("post-mutation session was revoked by the stale intent")
	}
	if _, ok := reloaded.Get(oldSession.Token); ok {
		t.Fatal("pre-mutation session resurfaced after delete_many")
	}
}

func TestRevocationIntentCancelledKeepsSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := registry.MarkUsernameRevocationPending("alice")
	if err != nil {
		t.Fatal(err)
	}
	// The mutation rolled back; the cancel must neutralize the intent on
	// replay, otherwise every restart would revoke a live user's sessions.
	if err := registry.CancelUsernameRevocation(intent); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); !ok {
		t.Fatal("cancelled revocation intent still revoked the session")
	}
}

func TestRevocationIntentSurvivesJournalCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.MarkUsernameRevocationPending("alice"); err != nil {
		t.Fatal(err)
	}
	// Force the journal to compact to a replace_all checkpoint while the
	// intent is still outstanding; the checkpoint must carry it forward or
	// the next start would resurrect the stale sessions.
	registry.mu.Lock()
	if err := registry.compactJournalLocked(); err != nil {
		registry.mu.Unlock()
		t.Fatal(err)
	}
	registry.mu.Unlock()

	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); ok {
		t.Fatal("revocation intent was lost across journal compaction")
	}
}

// TestAtomicUserUpdateJournalsRevocationIntentBeforeCommit proves the atomic
// handler leaves a durable revoke_username record even on the success path —
// the record that makes the crash window before delete_many recoverable.
func TestAtomicUserUpdateJournalsRevocationIntentBeforeCommit(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "viewer", PasswordHash: "current-viewer-hash", Role: "viewer", Locale: "en"},
		},
	}
	req := httptest.NewRequest("PUT", "/api/users/viewer", bytes.NewReader([]byte(`{"role":"admin","locale":"ru"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()

	state.handleAtomicUserUpdate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	journal, err := os.ReadFile(registry.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	intentIndex, deleteIndex := -1, -1
	for index, line := range bytes.Split(journal, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record sessionJournalRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("journal line %d: %v", index, err)
		}
		switch record.Operation {
		case "revoke_username":
			if record.Username == "viewer" && intentIndex == -1 {
				intentIndex = index
			}
		case "delete_many":
			if deleteIndex == -1 {
				deleteIndex = index
			}
		}
	}
	if intentIndex == -1 {
		t.Fatal("atomic user update never journaled a revocation intent")
	}
	if deleteIndex == -1 || deleteIndex < intentIndex {
		t.Fatalf("delete_many (line %d) must follow the intent (line %d)", deleteIndex, intentIndex)
	}
}
