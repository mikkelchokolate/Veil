package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSessionRegistryPersistsHashedSecretsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{
		Username:   "alice",
		Role:       "admin",
		UserAgent:  "session-test",
		RemoteAddr: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), session.Token) || strings.Contains(string(body), session.CSRFToken) {
		t.Fatalf("persistent session store contains raw bearer secrets: %s", body)
	}

	restarted, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := restarted.Get(session.Token)
	if !ok || got.Username != "alice" || got.Role != "admin" {
		t.Fatalf("reloaded session = %+v, ok=%v", got, ok)
	}
	if !restarted.ValidateCSRF(session.Token, session.CSRFToken) {
		t.Fatal("reloaded registry rejected the original CSRF token")
	}
}

func TestSessionRegistryEnforcesIdleAndAbsoluteExpiry(t *testing.T) {
	start := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	now := start
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return now }
	registry.idleTimeout = 30 * time.Minute
	registry.absoluteTimeout = 24 * time.Hour

	idleSession, err := registry.Create(SessionCreateInput{Username: "idle", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	now = start.Add(31 * time.Minute)
	if _, ok := registry.Get(idleSession.Token); ok {
		t.Fatal("session should expire after the idle timeout")
	}

	now = start
	absoluteSession, err := registry.Create(SessionCreateInput{Username: "absolute", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for step := 1; step < 72; step++ {
		now = start.Add(time.Duration(step) * 20 * time.Minute)
		if _, ok := registry.Get(absoluteSession.Token); !ok {
			t.Fatalf("session expired before absolute deadline at step %d", step)
		}
	}
	now = start.Add(24*time.Hour + time.Second)
	if _, ok := registry.Get(absoluteSession.Token); ok {
		t.Fatal("session should expire at the absolute deadline")
	}
}

func TestSessionRegistryRevokesByUserAndPreservesCurrentSession(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	aliceOne, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	aliceTwo, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	bob, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})

	if deleted, err := registry.DeleteByUsername("alice"); err != nil || deleted != 2 {
		t.Fatalf("DeleteByUsername deleted=%d err=%v", deleted, err)
	}
	if _, ok := registry.Get(aliceOne.Token); ok {
		t.Fatal("first alice session remains")
	}
	if _, ok := registry.Get(aliceTwo.Token); ok {
		t.Fatal("second alice session remains")
	}
	if _, ok := registry.Get(bob.Token); !ok {
		t.Fatal("unrelated user session was removed")
	}

	current, _ := registry.Create(SessionCreateInput{Username: "admin", Role: "admin"})
	other, _ := registry.Create(SessionCreateInput{Username: "other", Role: "viewer"})
	if deleted, err := registry.DeleteAllExcept(current.Token); err != nil || deleted != 2 {
		t.Fatalf("DeleteAllExcept deleted=%d err=%v", deleted, err)
	}
	if _, ok := registry.Get(current.Token); !ok {
		t.Fatal("current session should be preserved")
	}
	if _, ok := registry.Get(other.Token); ok {
		t.Fatal("other session should be removed")
	}
}

func TestSessionRegistryWritesOwnerOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("session store mode = %#o, want 0600", got)
	}
}

func TestRouterSessionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	info := ServerInfo{
		Version:      "test",
		Mode:         "server",
		StatePath:    filepath.Join(dir, "state.json"),
		KeyPath:      filepath.Join(dir, "state.key"),
		PanelAccess:  "local",
		PanelListen:  "127.0.0.1:2096",
		SetupAllowed: true,
	}
	first, _ := newTestRouter(info)

	setup := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(
		`{"username":"alice","password":"a-long-secure-password","backupAcknowledged":true}`,
	))
	setup.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()
	first.ServeHTTP(setupRec, setup)
	if setupRec.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", setupRec.Code, setupRec.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(
		`{"username":"alice","password":"a-long-secure-password"}`,
	))
	login.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	first.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var cookie *http.Cookie
	for _, candidate := range loginRec.Result().Cookies() {
		if candidate.Name == "veil_session" {
			cookie = candidate
		}
	}
	if cookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	restarted, _ := newTestRouter(info)
	status := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	status.AddCookie(cookie)
	statusRec := httptest.NewRecorder()
	restarted.ServeHTTP(statusRec, status)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var response struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.NewDecoder(statusRec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Authenticated || response.Username != "alice" || response.CSRFToken == "" {
		t.Fatalf("restart auth response = %+v", response)
	}
}

func TestUserAuthorityChangeRevokesExistingSessions(t *testing.T) {
	adminHash, _ := bcrypt.GenerateFromPassword([]byte("admin-password"), bcrypt.MinCost)
	bobHash, _ := bcrypt.GenerateFromPassword([]byte("bob-password"), bcrypt.MinCost)
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "alice", PasswordHash: string(adminHash), Role: "admin"},
			{Username: "bob", PasswordHash: string(bobHash), Role: "viewer"},
		},
	}
	bob, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})

	update := httptest.NewRequest(http.MethodPut, "/api/users/bob", strings.NewReader(
		`{"password":"new-bob-password","role":"viewer"}`,
	))
	update.Header.Set("Content-Type", "application/json")
	update = update.WithContext(context.WithValue(update.Context(), contextKeyRole, "admin"))
	updateRec := httptest.NewRecorder()
	state.handleUsersRoute(updateRec, update)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	if _, ok := registry.Get(bob.Token); ok {
		t.Fatal("password change did not revoke bob's session")
	}

	bob, _ = registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), contextKeyRole, "admin"))
	deleteRec := httptest.NewRecorder()
	state.handleUsersRoute(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, ok := registry.Get(bob.Token); ok {
		t.Fatal("user deletion did not revoke bob's session")
	}
}
