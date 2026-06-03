package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestRouterSecurityRecognizesBearerToken(t *testing.T) {
	if got := bearerToken("Bearer secret-token"); got != "secret-token" {
		t.Fatalf("bearerToken() = %q", got)
	}
	if got := bearerToken("bearer secret-token"); got != "secret-token" {
		t.Fatalf("bearerToken() = %q", got)
	}
	if got := bearerToken("Bearer  "); got != "" {
		t.Fatalf("bearerToken() expected empty, got %q", got)
	}
	if got := bearerToken("NotBearer token"); got != "" {
		t.Fatalf("bearerToken() expected empty, got %q", got)
	}
}

func TestSessionRegistry(t *testing.T) {
	registry := &SessionRegistry{
		sessions: make(map[string]Session),
	}

	sess := registry.NewSession("alice", "admin")
	if sess.Username != "alice" || sess.Role != "admin" {
		t.Fatalf("expected Username=alice, Role=admin; got %+v", sess)
	}
	if len(sess.Token) != 32 || len(sess.CSRFToken) != 32 {
		t.Fatalf("unexpected tokens lengths: %+v", sess)
	}

	gotSess, ok := registry.Get(sess.Token)
	if !ok || gotSess.Username != "alice" {
		t.Fatalf("failed to retrieve session")
	}

	// Expiry test
	sess.ExpiresAt = time.Now().Add(-1 * time.Minute)
	registry.sessions[sess.Token] = sess

	_, ok = registry.Get(sess.Token)
	if ok {
		t.Fatalf("session should have expired")
	}

	// Delete test
	sess2 := registry.NewSession("bob", "viewer")
	registry.Delete(sess2.Token)
	_, ok = registry.Get(sess2.Token)
	if ok {
		t.Fatalf("session should have been deleted")
	}
}

func TestAuthMiddleware(t *testing.T) {
	// Create mock management state
	state := &managementState{}
	state.users = []User{
		{
			Username:     "admin-user",
			PasswordHash: "fake",
			Role:         "admin",
		},
		{
			Username:     "viewer-user",
			PasswordHash: "fake",
			Role:         "viewer",
		},
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := authMiddleware(state, "static-secret-token", nextHandler)

	// 1. Public / non-API endpoints bypass auth
	req := httptest.NewRequest(http.MethodGet, "/public-page", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public page expected 200, got %d", w.Code)
	}

	// 2. Unauthenticated API request gets 401
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated expected 401, got %d", w.Code)
	}

	// 3. Static token authentication (X-Veil-Token)
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.Header.Set("X-Veil-Token", "static-secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("static token in header expected 200, got %d", w.Code)
	}

	// 4. Static token authentication (Authorization Bearer)
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.Header.Set("Authorization", "Bearer static-secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("static token in authorization expected 200, got %d", w.Code)
	}

	// 5. Dev mode bypass when no users exist
	emptyState := &managementState{}
	devHandler := authMiddleware(emptyState, "", nextHandler)
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	w = httptest.NewRecorder()
	devHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dev mode expected 200 when no users and no token, got %d", w.Code)
	}

	// 6. Cookie session
	sess := globalSessions.NewSession("admin-user", "admin")
	defer globalSessions.Delete(sess.Token)

	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cookie session GET expected 200, got %d", w.Code)
	}

	// 7. Cookie session mutating POST request without CSRF should fail (403)
	req = httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("mutating cookie session without CSRF expected 403, got %d", w.Code)
	}

	// 8. Cookie session mutating POST request with correct CSRF should pass (200)
	req = httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	req.Header.Set("X-CSRF-Token", sess.CSRFToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mutating cookie session with CSRF expected 200, got %d", w.Code)
	}

	// 9. RBAC: viewer role cannot perform mutating request even with CSRF (403)
	viewerSess := globalSessions.NewSession("viewer-user", "viewer")
	defer globalSessions.Delete(viewerSess.Token)

	req = httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSess.Token})
	req.Header.Set("X-CSRF-Token", viewerSess.CSRFToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer executing mutation expected 403, got %d", w.Code)
	}
}

func TestAuthLoginLogoutStatusEndpoints(t *testing.T) {
	// Create mock management state with one user
	hashed, _ := bcrypt.GenerateFromPassword([]byte("secret-pass"), 10)
	state := &managementState{}
	state.users = []User{
		{
			Username:     "alice",
			PasswordHash: string(hashed),
			Role:         "admin",
		},
	}

	// 1. Login success
	loginBody := `{"username":"alice","password":"secret-pass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	w := httptest.NewRecorder()
	state.handleLogin(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var loginResp struct {
		Success   bool   `json:"success"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(w.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if !loginResp.Success || loginResp.Username != "alice" || loginResp.Role != "admin" || loginResp.CSRFToken == "" {
		t.Fatalf("invalid login response contents: %+v", loginResp)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "veil_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("session cookie not set on login")
	}

	// 2. Auth status when logged in
	req = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	state.handleAuthStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status expected 200, got %d", w.Code)
	}
	var statusResp struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Role          string `json:"role"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.NewDecoder(w.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if !statusResp.Authenticated || statusResp.Username != "alice" || statusResp.Role != "admin" || statusResp.CSRFToken != loginResp.CSRFToken {
		t.Fatalf("invalid status response contents: %+v", statusResp)
	}

	// 3. Login failure
	badLoginBody := `{"username":"alice","password":"wrong-pass"}`
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(badLoginBody))
	w = httptest.NewRecorder()
	state.handleLogin(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login expected 401, got %d", w.Code)
	}

	// 4. Logout
	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	state.handleLogout(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout expected 200, got %d", w.Code)
	}

	logoutCookies := w.Result().Cookies()
	var expiredSessionCookie *http.Cookie
	for _, c := range logoutCookies {
		if c.Name == "veil_session" {
			expiredSessionCookie = c
			break
		}
	}
	if expiredSessionCookie == nil || expiredSessionCookie.MaxAge != -1 {
		t.Fatalf("logout did not expire session cookie: %+v", expiredSessionCookie)
	}

	// 5. Auth status when logged out
	req = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(expiredSessionCookie)
	w = httptest.NewRecorder()
	state.handleAuthStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status after logout expected 200, got %d", w.Code)
	}
	var loggedOutStatus struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(w.Body).Decode(&loggedOutStatus); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if loggedOutStatus.Authenticated {
		t.Fatalf("expected Authenticated=false after logout")
	}
}

func TestConstantTimeCompareCSRF(t *testing.T) {
	token := "correct-token"
	if subtle.ConstantTimeCompare([]byte(token), []byte("wrong-token")) == 1 {
		t.Fatalf("expected comparison to fail")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte("correct-token")) != 1 {
		t.Fatalf("expected comparison to succeed")
	}
}
