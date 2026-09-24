package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
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
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	registry.now = func() time.Time { return now }

	sess := mustCreateSession(t, registry, "alice", "admin")
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

	now = now.Add(31 * time.Minute)

	_, ok = registry.Get(sess.Token)
	if ok {
		t.Fatalf("session should have expired")
	}

	// Delete test
	sess2 := mustCreateSession(t, registry, "bob", "viewer")
	registry.Delete(sess2.Token)
	_, ok = registry.Get(sess2.Token)
	if ok {
		t.Fatalf("session should have been deleted")
	}
}

func TestSessionRegistryListsAndDeletesByStableID(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	alice := mustCreateSession(t, registry, "alice", "admin")
	bob := mustCreateSession(t, registry, "bob", "viewer")

	list := registry.List(alice.Token)
	if len(list) != 2 {
		t.Fatalf("expected two sessions, got %+v", list)
	}
	if !list[0].Current || list[0].Username != "alice" || list[0].ID == "" {
		t.Fatalf("current session should be listed first with stable id: %+v", list)
	}
	if list[1].Username != "bob" || list[1].Current {
		t.Fatalf("unexpected second session: %+v", list[1])
	}
	if err := registry.DeleteByID(sessionID(bob.Token)); err != nil {
		t.Fatal("failed to revoke bob session")
	}
	if _, ok := registry.Get(bob.Token); ok {
		t.Fatalf("bob session should be deleted")
	}
	if err := registry.DeleteByID("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing DeleteByID error = %v", err)
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

	// The stub echoes the authenticated identity the middleware injected so a
	// pass-through bug (empty user/role) cannot hide behind a bare 200 (#826).
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, _ := r.Context().Value(contextKeyUsername).(string)
		role, _ := r.Context().Value(contextKeyRole).(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok user=" + username + " role=" + role))
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=api-token role=admin") {
		t.Fatalf("static token in header expected 200 as api-token/admin, got %d %q", w.Code, w.Body.String())
	}

	// 4. Static token authentication (Authorization Bearer)
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.Header.Set("Authorization", "Bearer static-secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=api-token role=admin") {
		t.Fatalf("static token in authorization expected 200 as api-token/admin, got %d %q", w.Code, w.Body.String())
	}

	// 5. Dev mode bypass when no users exist
	emptyState := &managementState{}
	devHandler := authMiddleware(emptyState, "", nextHandler)
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	w = httptest.NewRecorder()
	devHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=dev-anonymous role=admin") {
		t.Fatalf("dev mode expected 200 as dev-anonymous/admin when no users and no token, got %d %q", w.Code, w.Body.String())
	}

	// 6. Cookie session
	sess := mustCreateSession(t, globalSessions, "admin-user", "admin")
	defer globalSessions.Delete(sess.Token)

	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=admin-user role=admin") {
		t.Fatalf("cookie session GET expected 200 as admin-user/admin, got %d %q", w.Code, w.Body.String())
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=admin-user role=admin") {
		t.Fatalf("mutating cookie session with CSRF expected 200 as admin-user/admin, got %d %q", w.Code, w.Body.String())
	}

	// 9. RBAC: viewer role cannot perform mutating request even with CSRF (403)
	viewerSess := mustCreateSession(t, globalSessions, "viewer-user", "viewer")
	defer globalSessions.Delete(viewerSess.Token)

	req = httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSess.Token})
	req.Header.Set("X-CSRF-Token", viewerSess.CSRFToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer executing mutation expected 403, got %d", w.Code)
	}

	// 10. A viewer may update only their own locale through the self-service route.
	req = httptest.NewRequest(http.MethodPost, "/api/auth/locale", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSess.Token})
	req.Header.Set("X-CSRF-Token", viewerSess.CSRFToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user=viewer-user role=viewer") {
		t.Fatalf("viewer locale update expected 200 as viewer-user/viewer, got %d %q", w.Code, w.Body.String())
	}
}

func TestRouterUsersEndpointAcceptsStaticAdminToken(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", AuthToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("X-Veil-Token", "secret-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected static admin token to access users, got %d: %s", w.Code, w.Body.String())
	}
	// A bare 200 could be any route — the body must be the users list (#838).
	var users []struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("users response is not a JSON list: %v (%s)", err, w.Body.String())
	}
	for _, user := range users {
		if user.Username == "" || (user.Role != "admin" && user.Role != "viewer") {
			t.Fatalf("users list contains malformed entry: %+v", user)
		}
	}
}

func TestAuthSessionsEndpointListsAndRevokesSessions(t *testing.T) {
	state := &managementState{}
	admin := mustCreateSession(t, globalSessions, "alice", "admin")
	viewer := mustCreateSession(t, globalSessions, "bob", "viewer")
	defer globalSessions.Delete(admin.Token)
	defer globalSessions.Delete(viewer.Token)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	w := httptest.NewRecorder()
	state.handleAuthSessions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected admin session list 200, got %d: %s", w.Code, w.Body.String())
	}
	var sessions []SessionInfo
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) < 2 || !sessions[0].Current || sessions[0].Username != "alice" {
		t.Fatalf("unexpected sessions list: %+v", sessions)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":"`+sessionID(viewer.Token)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	w = httptest.NewRecorder()
	state.handleAuthSessions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected revoke 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := globalSessions.Get(viewer.Token); ok {
		t.Fatalf("viewer session should be revoked")
	}
}

func TestAuthSessionsEndpointRejectsViewer(t *testing.T) {
	state := &managementState{}
	viewer := mustCreateSession(t, globalSessions, "bob", "viewer")
	defer globalSessions.Delete(viewer.Token)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	w := httptest.NewRecorder()
	state.handleAuthSessions(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected viewer sessions endpoint 403, got %d: %s", w.Code, w.Body.String())
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
			Locale:       "ru",
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
		Locale    string `json:"locale"`
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(w.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if !loginResp.Success || loginResp.Username != "alice" || loginResp.Role != "admin" || loginResp.Locale != "ru" || loginResp.CSRFToken == "" {
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
		Locale        string `json:"locale"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.NewDecoder(w.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if !statusResp.Authenticated || statusResp.Username != "alice" || statusResp.Role != "admin" || statusResp.Locale != "ru" || statusResp.CSRFToken != loginResp.CSRFToken {
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
	// The production CSRF gate is SessionRegistry.ValidateCSRF: it hashes the
	// presented token and constant-time-compares against the stored hash.
	// Exercising stdlib subtle.ConstantTimeCompare on raw strings proves
	// nothing about that path (#827).
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	csrf, _, err := registry.EnsureCSRFPersisted(sess.Token)
	if err != nil || csrf == "" {
		t.Fatalf("ensure csrf: %v", err)
	}

	if !registry.ValidateCSRF(sess.Token, csrf) {
		t.Fatal("ValidateCSRF rejected the session's own CSRF token")
	}
	if registry.ValidateCSRF(sess.Token, "wrong-csrf-token") {
		t.Fatal("ValidateCSRF accepted a wrong CSRF token")
	}
	if registry.ValidateCSRF(sess.Token, "") {
		t.Fatal("ValidateCSRF accepted an empty token")
	}
	if registry.ValidateCSRF("not-a-session", csrf) {
		t.Fatal("ValidateCSRF accepted an unknown session token")
	}
	// A different session's CSRF token must not validate against this session.
	other, err := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	otherCSRF, _, err := registry.EnsureCSRFPersisted(other.Token)
	if err != nil {
		t.Fatal(err)
	}
	if registry.ValidateCSRF(sess.Token, otherCSRF) {
		t.Fatal("ValidateCSRF accepted another session's CSRF token")
	}
}

func TestSecurityHeadersHSTSForIPHosts(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), false)

	cases := []struct {
		host     string
		wantHSTS bool
	}{
		{"45.157.233.54:25500", false},
		{"192.0.2.1", false},
		{"::1", false},
		{"panel.example.com", true},
		{"panel.example.com:443", true},
	}

	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = c.host
		req.TLS = &tls.ConnectionState{}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		got := w.Header().Get("Strict-Transport-Security")
		if c.wantHSTS {
			if got == "" {
				t.Fatalf("host %q: expected HSTS header", c.host)
			}
			if !strings.Contains(got, "max-age=63072000") {
				t.Fatalf("host %q: expected long HSTS policy, got %q", c.host, got)
			}
		} else {
			if got != "" {
				t.Fatalf("host %q: expected no HSTS header, got %q", c.host, got)
			}
		}
	}
}

// Under panelAccess=caddy the panel is reached through the managed Caddy TLS
// edge while the Go listener only sees plain loopback HTTP (r.TLS == nil).
// HSTS must still be emitted — matching the Secure-cookie edge treatment —
// otherwise the public site never sends it (#902).
func TestSecurityHeadersHSTSBehindCaddyEdge(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), true)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "panel.example.com"
	// r.TLS stays nil: this is what the request looks like after Caddy's
	// reverse_proxy hop.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=63072000") {
		t.Fatalf("expected HSTS behind caddy TLS edge, got %q", got)
	}

	// IP hosts still must not get HSTS (browsers would pin a bare IP).
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "192.0.2.1"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("expected no HSTS for IP host even behind caddy edge, got %q", got)
	}
}
