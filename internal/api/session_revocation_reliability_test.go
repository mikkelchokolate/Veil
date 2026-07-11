package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sessionRegistryWithFailingPersistence(t *testing.T) (*SessionRegistry, Session) {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.path = filepath.Join(parent, "sessions.json")
	return registry, session
}

func TestDeleteTokenPersistedRollsBackOnSaveFailure(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	deleted, err := registry.DeleteTokenPersisted(session.Token)
	if !deleted || err == nil {
		t.Fatalf("DeleteTokenPersisted deleted=%v err=%v", deleted, err)
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed persistent logout removed the in-memory session")
	}
}

func TestDeleteByIDPersistedRollsBackOnSaveFailure(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	deleted, err := registry.DeleteByIDPersisted(session.ID)
	if !deleted || err == nil {
		t.Fatalf("DeleteByIDPersisted deleted=%v err=%v", deleted, err)
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed persistent revocation removed the in-memory session")
	}
}

func TestLogoutReportsPersistenceFailureAndKeepsCookieSession(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	state := &managementState{sessions: registry, settings: Settings{PanelAccess: "local"}}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()

	state.handleLogoutWithSettingsSnapshot(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to persist logout") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed logout removed the active session")
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("failed logout unexpectedly changed cookies: %+v", cookies)
	}
}

func TestSessionRevokeReportsPersistenceFailureAndKeepsSession(t *testing.T) {
	registry, session := sessionRegistryWithFailingPersistence(t)
	state := &managementState{sessions: registry}
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":"`+session.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()

	state.handlePersistentAuthSessions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("failed revocation removed the active session")
	}
}
