package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionReadDoesNotSynchronouslyRewriteWholeStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return base }
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return base.Add(time.Minute) }
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("session unexpectedly missing")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("authenticated read synchronously rewrote the complete session store")
	}
}

func TestSessionPersistenceFailureCannotAuthenticateRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return base }
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return base.Add(time.Minute) }
	if _, ok := registry.Get(session.Token); ok {
		t.Fatal("session was accepted after LastSeen persistence failed")
	}
}

func TestSessionRoleAndExistenceAreRevalidatedAgainstCurrentUsers(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{users: []User{{Username: "alice", Role: "viewer"}}, sessions: registry}
	session, err := state.sessions.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(contextKeyRole).(string)
		_, _ = io.WriteString(w, role)
	})
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{}, next)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(); rec.Code != http.StatusOK || rec.Body.String() != "viewer" {
		t.Fatalf("stale admin role survived user role change: status=%d body=%q", rec.Code, rec.Body.String())
	}
	state.mu.Lock()
	state.users = nil
	state.mu.Unlock()
	if rec := request(); rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user session remained valid: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
