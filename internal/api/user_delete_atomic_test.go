package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func atomicUserDeleteRequest(username string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/users/"+username, nil)
	return req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
}

func TestAtomicUserDeleteStopsWhenSessionPersistenceFails(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: session.Username, PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	state.register(mux)
	mux.ServeHTTP(rec, atomicUserDeleteRequest(session.Username))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if responseErrorMessage(t, rec.Body.Bytes()) != "internal server error" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if len(state.users) != 2 {
		t.Fatalf("user was deleted despite revocation failure: %+v", state.users)
	}
	if !sessionPresentInMemory(registry, session.Token) {
		t.Fatal("session disappeared despite rollback")
	}
}

// TestAtomicUserDeleteRejectsViewerRole proves the admin gate runs before any
// mutation: a request carrying only viewer role context must get 403 and the
// user list must be untouched (#838).
func TestAtomicUserDeleteRejectsViewerRole(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "alice", PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/users/alice", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "viewer"))
	rec := httptest.NewRecorder()

	state.handleAtomicUserDelete(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users) != 2 {
		t.Fatalf("viewer request deleted a user: %+v", state.users)
	}
}

func TestAtomicUserDeleteRevokesSessionsAndDeletesUser(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "alice", PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	rec := httptest.NewRecorder()

	state.handleAtomicUserDelete(rec, atomicUserDeleteRequest("alice"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users) != 1 || state.users[0].Username != "admin" {
		t.Fatalf("unexpected users after delete: %+v", state.users)
	}
	if _, ok := registry.Get(session.Token); ok {
		t.Fatal("deleted user's session remains active")
	}
}

// TestAtomicUserDeleteKeepsSessionsWhenStateSaveFails covers #903: when the
// management state save fails, the in-memory delete rolls back and the user's
// sessions must still be valid — a failed delete can never log the user out.
func TestAtomicUserDeleteKeepsSessionsWhenStateSaveFails(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions:  registry,
		statePath: filepath.Join(blocked, "state.json"),
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "alice", PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	rec := httptest.NewRecorder()

	state.handleAtomicUserDelete(rec, atomicUserDeleteRequest("alice"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users) != 2 {
		t.Fatalf("failed delete removed the user: %+v", state.users)
	}
	if !sessionPresentInMemory(registry, session.Token) {
		t.Fatal("failed delete revoked the user's session")
	}
}

// TestAtomicUserDeleteRestoresUserWhenRevocationFails covers the second half
// of #903: when the delete commits but session persistence fails, the user
// record is restored so the request stays all-or-nothing.
func TestAtomicUserDeleteRestoresUserWhenRevocationFails(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.path = filepath.Join(blocked, "sessions.json")
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "alice", PasswordHash: "alice-hash", Role: "viewer", Locale: "en"},
		},
	}
	rec := httptest.NewRecorder()

	state.handleAtomicUserDelete(rec, atomicUserDeleteRequest("alice"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if msg := responseErrorMessage(t, rec.Body.Bytes()); msg != "internal server error" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	restored := false
	for _, user := range state.users {
		if user.Username == "alice" {
			restored = true
			if user.Role != "viewer" || user.PasswordHash != "alice-hash" {
				t.Fatalf("restored user lost fields: %+v", user)
			}
		}
	}
	if !restored {
		t.Fatalf("failed revocation left the user deleted: %+v", state.users)
	}
	if !sessionPresentInMemory(registry, session.Token) {
		t.Fatal("failed revocation removed the in-memory session")
	}
}

func TestAtomicUserDeletePreservesLastAdministrator(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users:    []User{{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"}},
	}
	rec := httptest.NewRecorder()

	state.handleAtomicUserDelete(rec, atomicUserDeleteRequest("admin"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users) != 1 {
		t.Fatalf("last admin was deleted: %+v", state.users)
	}
	if !sessionPresentInMemory(registry, session.Token) {
		t.Fatal("last admin session was revoked")
	}
}
