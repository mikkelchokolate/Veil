package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidatePersistedSessionsRemovesSnapshotAndJournal(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	sessionPath := filepath.Join(root, "sessions.json")
	journalPath := sessionPath + ".journal"
	if err := os.WriteFile(statePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journalPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte("{\"operation\":\"upsert\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := InvalidatePersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("sessions.json still present: %v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("session journal still present: %v", err)
	}

	reloaded, err := NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); ok {
		t.Fatal("pre-restore session remained authorized")
	}
}

func TestInvalidatePersistedSessionsIgnoresMissingStore(t *testing.T) {
	if err := InvalidatePersistedSessions(filepath.Join(t.TempDir(), "state.json")); err != nil {
		t.Fatal(err)
	}
}
