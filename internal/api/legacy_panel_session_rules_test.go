package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/panel"
)

// legacyPanelFor renders the legacy (no-SPA) GET / for the given cookie.
func legacyPanelFor(t *testing.T, state *managementState, token string) string {
	t.Helper()
	routes := PanelRoutes{Info: ServerInfo{Version: "test"}, State: state}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: token})
	rec := httptest.NewRecorder()
	routes.handlePanel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// legacyLoginMarker returns the first non-trivial line of the login page that
// the authenticated panel HTML does not contain, so a body can be classified
// as login vs. panel without depending on the CSP nonce rewrite.
func legacyLoginMarker(t *testing.T) string {
	t.Helper()
	login := panel.ReliableLoginHTML("", "en")
	authed := panelHTMLForCatalog("", "0123456789abcdef0123456789abcdef", "en", NewManagedRuntimeCatalog())
	for _, line := range strings.Split(login, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 24 && !strings.Contains(line, "<script") && !strings.Contains(line, "<style") && !strings.Contains(authed, line) {
			return line
		}
	}
	t.Fatal("no login-only marker line found")
	return ""
}

func assertLegacyLoginPage(t *testing.T, state *managementState, token string) {
	t.Helper()
	body := legacyPanelFor(t, state, token)
	if !strings.Contains(body, legacyLoginMarker(t)) {
		t.Fatalf("legacy panel did not render the login page for an unauthorized session")
	}
	if _, ok := state.sessionRegistry().Get(token); ok {
		t.Fatal("unauthorized session was not revoked by the legacy panel")
	}
	tokenHash := hashSessionSecret(token)
	registry := state.sessionRegistry()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if csrf := registry.rawCSRF[tokenHash]; csrf != "" {
		t.Fatalf("legacy panel minted a CSRF token for an unauthorized session")
	}
}

// The legacy panel is the call-site twin of /api/auth/status (#1336): a
// deleted user's session must get the login page, be revoked, and not have a
// CSRF hash minted and persisted for it (#1266).
func TestLegacyPanelRevokesSessionOfDeletedUser(t *testing.T) {
	state := orphanStatusTestState(t, []User{{Username: "alice", Role: "admin", Locale: "en"}, {Username: "bob", Role: "admin", Locale: "en"}})
	live := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if body := legacyPanelFor(t, state, live.Token); strings.Contains(body, legacyLoginMarker(t)) {
		t.Fatal("a live user's session should get the authenticated panel")
	}
	if _, ok := state.sessionRegistry().Get(live.Token); !ok {
		t.Fatal("a live user's session must survive the legacy panel")
	}

	orphan := mustCreateSession(t, state.sessionRegistry(), "bob", "admin")
	state.mu.Lock()
	state.users = state.users[:1] // delete bob
	state.mu.Unlock()
	assertLegacyLoginPage(t, state, orphan.Token)
}

// A session minted before the owner armed a second factor lacks the
// second-factor mark; the middleware revokes it, so the legacy panel must too.
func TestLegacyPanelRevokesFactorDeficientSession(t *testing.T) {
	state := orphanStatusTestState(t, []User{{Username: "alice", Role: "admin", Locale: "en", TOTPEnabled: true}})
	sess := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	assertLegacyLoginPage(t, state, sess.Token)
}
