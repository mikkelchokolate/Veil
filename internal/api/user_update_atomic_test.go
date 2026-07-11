package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestAtomicUserUpdateReturnsNotFoundInsideMutation(t *testing.T) {
	state := atomicUserUpdateState(t)
	req := atomicUserUpdateRequest("/api/users/missing", `{"role":"viewer","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
