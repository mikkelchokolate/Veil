package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionJournalIgnoresOnlyTornFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(SessionCreateInput{Username: "first", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	journalSession, err := registry.Create(SessionCreateInput{Username: "journal", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry.journalPath(), append(mustReadFile(t, registry.journalPath()), []byte(`{"operation":"delete","tokenHash":"torn`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatalf("torn final record prevented recovery: %v", err)
	}
	if _, ok := reloaded.Get(journalSession.Token); !ok {
		t.Fatal("valid journal prefix was not replayed")
	}
	if err := os.WriteFile(registry.journalPath(), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionRegistry(path); err == nil {
		t.Fatal("complete corrupt journal record was ignored")
	}
}

func TestSessionJournalCompactsToCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "compact", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	hash := hashSessionSecret(session.Token)
	registry.mu.Lock()
	record := registry.sessions[hash]
	for index := 0; index < 5000; index++ {
		record.UserAgent = string(bytes.Repeat([]byte{'x'}, 300))
		if err := registry.persistUpsertLocked(record); err != nil {
			registry.mu.Unlock()
			t.Fatal(err)
		}
	}
	registry.mu.Unlock()
	info, err := os.Stat(registry.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1024*1024 {
		t.Fatalf("journal was not compacted: %d bytes", info.Size())
	}
	if _, err := NewSessionRegistry(path); err != nil {
		t.Fatalf("compacted checkpoint did not reload: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
