package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func csrfPersistenceFailureRegistry(t *testing.T) (*SessionRegistry, Session, storedSession) {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := hashSessionSecret(session.Token)
	registry.mu.Lock()
	original := registry.sessions[tokenHash]
	delete(registry.rawCSRF, tokenHash)
	registry.path = blockedSessionStorePath(t)
	registry.mu.Unlock()
	return registry, session, original
}

func TestEnsureCSRFPersistedRollsBackHashOnSaveFailure(t *testing.T) {
	registry, session, original := csrfPersistenceFailureRegistry(t)

	csrf, ok, err := registry.EnsureCSRFPersisted(session.Token)
	if err == nil || ok || csrf != "" {
		t.Fatalf("EnsureCSRFPersisted csrf=%q ok=%v err=%v", csrf, ok, err)
	}

	tokenHash := hashSessionSecret(session.Token)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if got := registry.sessions[tokenHash].CSRFHash; got != original.CSRFHash {
		t.Fatalf("CSRF hash changed after failed persistence: got %q want %q", got, original.CSRFHash)
	}
	if got := registry.rawCSRF[tokenHash]; got != "" {
		t.Fatalf("raw CSRF token leaked after failed persistence: %q", got)
	}
}

func TestEffectiveAuthStatusReportsCSRFPersistenceFailure(t *testing.T) {
	registry, session, original := csrfPersistenceFailureRegistry(t)
	state := &managementState{
		sessions: registry,
		users:    []User{{Username: "alice", Role: "admin", Locale: "en"}},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()

	state.handleEffectiveAuthStatus(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to refresh session") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	tokenHash := hashSessionSecret(session.Token)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if got := registry.sessions[tokenHash].CSRFHash; got != original.CSRFHash {
		t.Fatalf("auth status left a changed CSRF hash: got %q want %q", got, original.CSRFHash)
	}
}
