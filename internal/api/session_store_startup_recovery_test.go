package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptSessionStoreRecoversWithoutEphemeralFallback(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	sessionPath := filepath.Join(root, "sessions.json")
	if err := os.WriteFile(sessionPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	state := newManagementState(ServerInfo{
		StatePath: statePath,
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	if state.sessions.path != sessionPath {
		t.Fatalf("session path=%q want %q", state.sessions.path, sessionPath)
	}

	session, err := state.sessions.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatalf("recover session store: %v", err)
	}
	reloaded, err := NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatalf("reload recovered session store: %v", err)
	}
	if _, ok := reloaded.Get(session.Token); !ok {
		t.Fatal("recovered session was not persisted")
	}
}

func TestUnreadableSessionStoreDoesNotAcceptEphemeralSessions(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(blockedParent, "state.json")
	sessionPath := filepath.Join(blockedParent, "sessions.json")

	state := newManagementState(ServerInfo{
		StatePath: statePath,
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	if state.sessions.path != sessionPath {
		t.Fatalf("session path=%q want %q", state.sessions.path, sessionPath)
	}
	if _, err := state.sessions.Create(SessionCreateInput{Username: "alice", Role: "admin"}); err == nil {
		t.Fatal("session creation succeeded despite an unusable persistence path")
	}
	state.sessions.mu.Lock()
	defer state.sessions.mu.Unlock()
	if len(state.sessions.sessions) != 0 {
		t.Fatalf("failed persistent create left %d in-memory sessions", len(state.sessions.sessions))
	}
}
