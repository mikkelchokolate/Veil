package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestLoginFlowEndToEnd exercises the production login handler through the
// registered mux: response body fields, the session cookie contract, and a
// follow-up auth/status that resolves the new session (#825).
func TestLoginFlowEndToEnd(t *testing.T) {
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, "secure-password-123"),
		Role:         "admin",
		Locale:       "ru",
	})
	// Control the login-backoff clock so the failure-then-success sequence is
	// deterministic: a failed attempt imposes a short per-(IP, username)
	// backoff that an immediate retry must obey.
	now := time.Now()
	state.loginBackoffNow = func() time.Time { return now }
	mux := http.NewServeMux()
	state.register(mux)
	router := authMiddleware(state, "", mux)

	// Bad credentials: 401, no cookie, no session.
	bad := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"wrong"}`))
	bad.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	router.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status=%d body=%s", badRec.Code, badRec.Body.String())
	}
	if badRec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("failed login set a cookie: %q", badRec.Header().Get("Set-Cookie"))
	}
	if sessions := state.sessionRegistry().List(""); len(sessions) != 0 {
		t.Fatalf("failed login created sessions: %+v", sessions)
	}

	// An immediate retry — even with the right password — is throttled by the
	// post-failure backoff and must surface 429 with Retry-After.
	retry := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secure-password-123"}`))
	retry.Header.Set("Content-Type", "application/json")
	retry.RemoteAddr = bad.RemoteAddr
	retryRec := httptest.NewRecorder()
	router.ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate retry status=%d body=%s", retryRec.Code, retryRec.Body.String())
	}
	if retryRec.Header().Get("Retry-After") == "" {
		t.Fatal("throttled retry missing Retry-After header")
	}
	if retryRec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("throttled retry set a cookie: %q", retryRec.Header().Get("Set-Cookie"))
	}

	// Once the backoff window elapses the correct credentials succeed.
	now = now.Add(2 * time.Second)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secure-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success   bool   `json:"success"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		Locale    string `json:"locale"`
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	if !body.Success || body.Username != "alice" || body.Role != "admin" || body.Locale != "ru" {
		t.Fatalf("login body wrong: %+v", body)
	}
	if body.CSRFToken == "" {
		t.Fatal("login response missing csrfToken")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "veil_session" || cookies[0].Value == "" {
		t.Fatalf("login cookies = %+v", cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("session cookie attributes wrong: %+v", cookie)
	}
	sess, ok := state.sessionRegistry().Get(cookie.Value)
	if !ok || sess.Username != "alice" || sess.Role != "admin" {
		t.Fatalf("session cookie does not resolve to the logged-in session: %+v ok=%v", sess, ok)
	}

	// The new cookie session authenticates and reports the effective identity.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	statusReq.AddCookie(cookie)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("auth status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Role          string `json:"role"`
		Locale        string `json:"locale"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.NewDecoder(statusRec.Body).Decode(&status); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !status.Authenticated || status.Username != "alice" || status.Role != "admin" || status.Locale != "ru" {
		t.Fatalf("auth status wrong: %+v", status)
	}
	if status.CSRFToken != body.CSRFToken {
		t.Fatalf("status csrfToken does not match the login-issued token")
	}
}

// assertLoginSetsSessionCookieAndCSRF pins both halves of a usable login —
// the veil_session cookie and the csrfToken in the JSON body — so a bare 200
// cannot pass (#827).
func assertLoginSetsSessionCookieAndCSRF(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("login body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.CSRFToken == "" {
		t.Fatal("login response missing csrfToken")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "veil_session" || cookies[0].Value == "" || !cookies[0].HttpOnly {
		t.Fatalf("login cookies = %+v", cookies)
	}
}
