package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLogoutRequiresCSRFWithLiveSession locks in audit #198: a mutating public
// endpoint (POST /api/auth/logout) carrying a live cookie session must reject
// cross-site requests that do not prove CSRF. Without the gate, any website
// could revoke a victim's panel session (CSRF logout).
func TestLogoutRequiresCSRFWithLiveSession(t *testing.T) {
	// Build the router + authMiddleware exactly as the production composition
	// does, so the CSRF gate for live sessions on mutating public endpoints
	// is exercised.
	state := newManagementState(ServerInfo{Mode: "dev"})
	mux := http.NewServeMux()
	state.register(mux)
	router := authMiddleware(state, "", mux)

	// dev mode with no users: login may not create a session; use the session
	// registry directly instead (mirrors a live cookie session).
	sess, err := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	csrf, _, err := state.sessionRegistry().EnsureCSRFPersisted(sess.Token)
	if err != nil || csrf == "" {
		t.Fatalf("ensure csrf: %v", err)
	}

	// Cross-site logout: cookie present, NO CSRF header -> 403.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// Legit logout: cookie + valid CSRF header -> 200 and session revoked.
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req2.AddCookie(&http.Cookie{Name: "veil_session", Value: sess.Token})
	req2.Header.Set("X-CSRF-Token", csrf)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("logout with CSRF = %d, want 200; body = %s", rec2.Code, rec2.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(sess.Token); ok {
		t.Fatal("session still live after logout")
	}

	// Anonymous logout (no cookie) stays allowed: public short-circuit.
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("anonymous logout = %d, want 200; body = %s", rec3.Code, rec3.Body.String())
	}
}
