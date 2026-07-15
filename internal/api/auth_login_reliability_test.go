package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func loginReliabilityPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func loginReliabilityState(t *testing.T, user User) *managementState {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	return &managementState{
		sessions: registry,
		settings: Settings{
			PanelAccess: "local",
		},
		users: []User{user},
	}
}

func TestLoginSnapshotRejectsPasswordChangedAfterVerification(t *testing.T) {
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, "old-password-123"),
		Role:         "admin",
		Locale:       "en",
	})
	snapshot := state.snapshotLoginCredentials("alice")
	if !snapshot.passwordMatches("old-password-123") {
		t.Fatal("snapshot did not accept the original password")
	}

	state.mu.Lock()
	state.users[0].PasswordHash = loginReliabilityPasswordHash(t, "new-password-123")
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	_, _, _, _, err := state.createSessionForLoginSnapshot(snapshot, req)
	if !errors.Is(err, errLoginCredentialsChanged) {
		t.Fatalf("createSessionForLoginSnapshot() error = %v", err)
	}
	if sessions := state.sessionRegistry().List(""); len(sessions) != 0 {
		t.Fatalf("stale password created sessions: %+v", sessions)
	}
}

func TestLoginSnapshotUsesCurrentRoleAndLocale(t *testing.T) {
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, "secure-password-123"),
		Role:         "admin",
		Locale:       "en",
	})
	snapshot := state.snapshotLoginCredentials("alice")
	if !snapshot.passwordMatches("secure-password-123") {
		t.Fatal("snapshot did not accept the password")
	}

	state.mu.Lock()
	state.users[0].Role = "viewer"
	state.users[0].Locale = "ru"
	state.settings.PanelAccess = "caddy"
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	session, role, locale, panelAccess, err := state.createSessionForLoginSnapshot(snapshot, req)
	if err != nil {
		t.Fatalf("createSessionForLoginSnapshot() error = %v", err)
	}
	if role != "viewer" || session.Role != "viewer" || locale != "ru" || panelAccess != "caddy" {
		t.Fatalf("stale login identity: role=%q sessionRole=%q locale=%q panelAccess=%q", role, session.Role, locale, panelAccess)
	}
}

func TestFallbackLoginSnapshotRejectsChangedSettings(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		settings: Settings{
			NaivePassword: "fallback-password-123",
			PanelAccess:   "local",
		},
	}
	snapshot := state.snapshotLoginCredentials("admin")
	if !snapshot.passwordMatches("fallback-password-123") {
		t.Fatal("fallback snapshot did not accept the original password")
	}

	state.mu.Lock()
	state.settings.NaivePassword = "replacement-password-123"
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	_, _, _, _, err = state.createSessionForLoginSnapshot(snapshot, req)
	if !errors.Is(err, errLoginCredentialsChanged) {
		t.Fatalf("createSessionForLoginSnapshot() error = %v", err)
	}
}

func TestRegisteredLoginUsesRevalidationRoute(t *testing.T) {
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, "secure-password-123"),
		Role:         "admin",
		Locale:       "en",
	})
	mux := http.NewServeMux()
	state.register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	if handler, pattern := mux.Handler(req); handler == nil || pattern != "/api/auth/login" {
		t.Fatalf("registered login pattern = %q", pattern)
	}
}
