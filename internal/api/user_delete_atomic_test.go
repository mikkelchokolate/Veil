package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !strings.Contains(rec.Body.String(), errSessionRevocationPersistence.Error()) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if len(state.users) != 2 {
		t.Fatalf("user was deleted despite revocation failure: %+v", state.users)
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("session disappeared despite rollback")
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
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("last admin session was revoked")
	}
}
