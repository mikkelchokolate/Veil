package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMinInt(t *testing.T) {
	if minInt(1, 2) != 1 {
		t.Fatal("minInt(1,2) should be 1")
	}
	if minInt(3, 2) != 2 {
		t.Fatal("minInt(3,2) should be 2")
	}
	if minInt(-1, 0) != -1 {
		t.Fatal("minInt(-1,0) should be -1")
	}
}

func TestWriteSessionFileFailsWhenParentIsFile(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeSessionFile(filepath.Join(parent, "sessions.json"), []byte("{}"))
	if err == nil {
		t.Fatal("expected error when parent path is a file")
	}
}

func TestSessionRegistryCreateFailsWhenRandomReaderFails(t *testing.T) {
	old := randomReader
	randomReader = func(b []byte) (int, error) {
		return 0, errors.New("random failure")
	}
	t.Cleanup(func() { randomReader = old })

	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err == nil {
		t.Fatal("expected create error when random reader fails")
	}
}

func TestSessionRegistryLoadSkipsInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sessions.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewSessionRegistry(path)
	if err == nil {
		t.Fatal("expected error loading invalid session JSON")
	}
}

func TestSessionRegistryLoadSkipsExpiredSessions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sessions.json")
	body := `{"version":1,"sessions":[{"tokenHash":"deadbeef","username":"alice","createdAt":"2000-01-01T00:00:00Z","lastSeenAt":"2000-01-01T00:00:00Z","idleExpiresAt":"2000-01-01T00:00:00Z","expiresAt":"2000-01-02T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	list := registry.List("")
	if len(list) != 0 {
		t.Fatalf("expected expired session to be skipped, got %+v", list)
	}
}
