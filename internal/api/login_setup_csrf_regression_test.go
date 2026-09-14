package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginDoesNotRequireCSRFWithLiveSession(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("secret-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	router, reloader := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.mu.Lock()
	state.users = []User{{Username: "alice", PasswordHash: string(hashed), Role: "admin"}}
	state.mu.Unlock()

	leftover, err := state.sessionRegistry().Create(SessionCreateInput{Username: "stale", Role: "admin"})
	if err != nil {
		t.Fatalf("create leftover session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: leftover.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("login with leftover session cookie required CSRF: %s", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"wrong-pass"}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.AddCookie(&http.Cookie{Name: "veil_session", Value: leftover.Token})
	badRec := httptest.NewRecorder()
	router.ServeHTTP(badRec, bad)
	if badRec.Code == http.StatusForbidden {
		t.Fatalf("invalid login with leftover session cookie required CSRF: %s", badRec.Body.String())
	}
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestSetupCompleteDoesNotRequireCSRFWithLiveSession(t *testing.T) {
	dir := t.TempDir()
	router, reloader := newTestRouter(ServerInfo{
		Version:      "test",
		Mode:         "server",
		StatePath:    filepath.Join(dir, "state.json"),
		KeyPath:      filepath.Join(dir, "state.key"),
		PanelAccess:  "local",
		PanelListen:  "127.0.0.1:2096",
		SetupAllowed: true,
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })

	leftover, err := state.sessionRegistry().Create(SessionCreateInput{Username: "stale", Role: "admin"})
	if err != nil {
		t.Fatalf("create leftover session: %v", err)
	}

	req := newSetupCompleteRequest()
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: leftover.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "CSRF") {
		t.Fatalf("setup with leftover session cookie required CSRF: %s", rec.Body.String())
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", rec.Code, rec.Body.String())
	}
}
