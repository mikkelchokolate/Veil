package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func atomicUserUpdateState(t *testing.T) *managementState {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	return &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "viewer", PasswordHash: "current-viewer-hash", Role: "viewer", Locale: "en"},
		},
	}
}

func atomicUserUpdateRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
}

func TestAtomicUserUpdatePreservesCurrentPasswordWhenOmitted(t *testing.T) {
	state := atomicUserUpdateState(t)
	req := atomicUserUpdateRequest("/api/users/viewer", `{"role":"admin","locale":"ru"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got User
	for _, user := range state.users {
		if user.Username == "viewer" {
			got = user
			break
		}
	}
	if got.PasswordHash != "current-viewer-hash" {
		t.Fatalf("password hash changed to %q", got.PasswordHash)
	}
	if got.Role != "admin" || got.Locale != "ru" {
		t.Fatalf("user fields were not updated: %+v", got)
	}
}

func TestAtomicUserUpdateAppliesExplicitPassword(t *testing.T) {
	state := atomicUserUpdateState(t)
	password := "new-password-123"
	req := atomicUserUpdateRequest("/api/users/viewer", `{"password":"`+password+`","role":"viewer","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username != "viewer" {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			t.Fatalf("updated password hash does not match: %v", err)
		}
		return
	}
	t.Fatal("updated user not found")
}

func TestAtomicUserUpdateRevokesExistingSessions(t *testing.T) {
	state := atomicUserUpdateState(t)
	session, err := state.sessionRegistry().Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	req := atomicUserUpdateRequest("/api/users/viewer", `{"role":"viewer","locale":"ru"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(session.Token); ok {
		t.Fatal("updated user session was not revoked")
	}
}

// TestAtomicUserUpdateRejectsViewerRole proves the admin gate runs before any
// mutation: a request carrying only viewer role context must get 403 and the
// user record must be untouched (#838).
func TestAtomicUserUpdateRejectsViewerRole(t *testing.T) {
	state := atomicUserUpdateState(t)
	req := httptest.NewRequest(http.MethodPut, "/api/users/viewer", strings.NewReader(`{"role":"admin","locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "viewer"))
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "viewer" && (user.Role != "viewer" || user.Locale != "en") {
			t.Fatalf("viewer request mutated the user: %+v", user)
		}
	}
}

func TestAtomicUserUpdateReturnsNotFoundInsideMutation(t *testing.T) {
	state := atomicUserUpdateState(t)
	req := atomicUserUpdateRequest("/api/users/missing", `{"role":"viewer","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestAtomicUserUpdateKeepsSessionsWhenStateSaveFails covers #903: the user
// mutation must persist BEFORE session revocation runs. When the management
// state save fails, the in-memory update rolls back and the user's sessions
// must still be valid — a failed update can never log the user out.
func TestAtomicUserUpdateKeepsSessionsWhenStateSaveFails(t *testing.T) {
	state := atomicUserUpdateState(t)
	session, err := state.sessionRegistry().Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	// Point the state file at a path that can never be created (its parent is
	// a regular file) so every save fails deterministically.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.statePath = filepath.Join(blocked, "state.json")

	req := atomicUserUpdateRequest("/api/users/viewer", `{"role":"admin","locale":"ru"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// The update rolled back.
	for _, user := range state.users {
		if user.Username == "viewer" && (user.Role != "viewer" || user.Locale != "en") {
			t.Fatalf("failed update left mutated user: %+v", user)
		}
	}
	// The session must NOT have been revoked by the failed update.
	if _, ok := state.sessionRegistry().Get(session.Token); !ok {
		t.Fatal("failed user update revoked the user's session")
	}
}

// TestAtomicUserUpdateRestoresUserWhenRevocationFails covers the second half
// of #903: when the user save succeeds but session persistence fails, the
// committed user change is restored so the request stays all-or-nothing.
func TestAtomicUserUpdateRestoresUserWhenRevocationFails(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	// Force every session persistence write to fail.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.path = filepath.Join(blocked, "sessions.json")
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "viewer", PasswordHash: "current-viewer-hash", Role: "viewer", Locale: "en"},
		},
	}
	req := atomicUserUpdateRequest("/api/users/viewer", `{"role":"admin","locale":"ru"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if msg := responseErrorMessage(t, rec.Body.Bytes()); msg != "internal server error" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "viewer" && (user.Role != "viewer" || user.Locale != "en" || user.PasswordHash != "current-viewer-hash") {
			t.Fatalf("failed revocation left a mutated user: %+v", user)
		}
	}
	if !sessionPresentInMemory(registry, session.Token) {
		t.Fatal("failed revocation removed the in-memory session")
	}
}
