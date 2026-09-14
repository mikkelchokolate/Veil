package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func cookieAppliesToPath(cookiePath, requestPath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}
	if cookiePath == "/" {
		return strings.HasPrefix(requestPath, "/")
	}
	if requestPath == cookiePath {
		return true
	}
	if strings.HasSuffix(cookiePath, "/") {
		return strings.HasPrefix(requestPath, cookiePath)
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return len(requestPath) == len(cookiePath) || requestPath[len(cookiePath)] == '/'
}

func sessionSetCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "veil_session" {
			return cookie
		}
	}
	return nil
}

func TestLoginSessionCookieIsScopedToWebBasePath(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("secret-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		PanelAccess: "caddy",
		WebBasePath: "/panel-secret/",
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.mu.Lock()
	state.users = []User{{Username: "alice", PasswordHash: string(hashed), Role: "admin"}}
	state.settings.WebBasePath = "/panel-secret/"
	state.settings.PanelAccess = "caddy"
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/panel-secret/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionSetCookie(rec)
	if cookie == nil {
		t.Fatal("login did not set veil_session")
	}
	if cookie.Path != "/panel-secret/" {
		t.Fatalf("login cookie path=%q, want /panel-secret/", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("login SameSite=%v, want Lax", cookie.SameSite)
	}
	if cookieAppliesToPath(cookie.Path, "/s/example-token") {
		t.Fatalf("session cookie path %q must not apply to public /s/*", cookie.Path)
	}
	if !cookieAppliesToPath(cookie.Path, "/panel-secret/api/auth/status") {
		t.Fatalf("session cookie path %q must apply to the Panel mount", cookie.Path)
	}

	var loginResp struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatal(err)
	}
	logout := httptest.NewRequest(http.MethodPost, "/panel-secret/api/auth/logout", nil)
	logout.AddCookie(cookie)
	logout.Header.Set("X-CSRF-Token", loginResp.CSRFToken)
	logoutRec := httptest.NewRecorder()
	router.ServeHTTP(logoutRec, logout)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
	expired := sessionSetCookie(logoutRec)
	if expired == nil {
		t.Fatal("logout did not expire veil_session")
	}
	if expired.Path != cookie.Path {
		t.Fatalf("logout path=%q, want %q", expired.Path, cookie.Path)
	}
	if expired.SameSite != http.SameSiteLaxMode {
		t.Fatalf("logout SameSite=%v, want Lax", expired.SameSite)
	}
	if !expired.Secure || !expired.HttpOnly {
		t.Fatalf("logout cookie missing Secure/HttpOnly: %+v", expired)
	}
	if expired.MaxAge >= 0 {
		t.Fatalf("logout MaxAge=%d, want expired", expired.MaxAge)
	}
}

func TestRootMountSessionCookieKeepsOriginPath(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("secret-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		PanelAccess: "local",
		WebBasePath: "/",
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.mu.Lock()
	state.users = []User{{Username: "alice", PasswordHash: string(hashed), Role: "admin"}}
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionSetCookie(rec)
	if cookie == nil || cookie.Path != "/" {
		t.Fatalf("root mount cookie=%+v, want Path=/", cookie)
	}
}

func TestLogoutCookieRepeatsLoginSameSite(t *testing.T) {
	state := &managementState{settings: Settings{PanelAccess: "caddy", WebBasePath: "/panel-secret/"}}
	mux := http.NewServeMux()
	state.register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "Path=/panel-secret/") || !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("logout cookie=%q", cookie)
	}
}
