package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sabotageSessionJournal(t *testing.T, registry *SessionRegistry) {
	t.Helper()
	path := registry.journalPath()
	_ = os.Remove(path)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
}

func TestGetKeepsLiveSessionWhenLastSeenPersistFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return time.Now().UTC().Add(time.Minute) }
	registry.persistInterval = time.Nanosecond
	sabotageSessionJournal(t, registry)

	got, ok := registry.Get(session.Token)
	if !ok {
		t.Fatal("LastSeen persist failure unauthenticated a live session")
	}
	if got.Username != "alice" || got.Role != "admin" {
		t.Fatalf("session=%+v", got)
	}
}

func TestAuthMiddlewareKeepsCookieWhenLastSeenPersistFails(t *testing.T) {
	root := t.TempDir()
	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	state.users = []User{{Username: "alice", Role: "admin"}}
	session, err := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	registry := state.sessionRegistry()
	registry.now = func() time.Time { return time.Now().UTC().Add(time.Minute) }
	registry.persistInterval = time.Nanosecond
	sabotageSessionJournal(t, registry)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: false}, mux)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("auth status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateStillFailsWhenSessionPersistFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	sabotageSessionJournal(t, registry)
	if _, err := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"}); err == nil {
		t.Fatal("create succeeded despite journal persist failure")
	}
}

func TestValidSnapshotSurvivesCorruptSessionJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry.journalPath(), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatalf("valid snapshot discarded: %v", err)
	}
	if _, ok := reloaded.Get(session.Token); !ok {
		t.Fatal("snapshot session missing after corrupt journal")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("live snapshot was removed: %v", err)
	}
}

func TestProductionStartupKeepsSnapshotWhenJournalIsCorrupt(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions.json")
	registry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry.journalPath(), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	state.users = []User{{Username: "alice", Role: "admin"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: false}, mux)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("startup auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("production startup removed the live snapshot: %v", err)
	}
}
