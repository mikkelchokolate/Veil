package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// restartedLegacyPanelState returns a state whose file-backed session
// registry was reopened from disk after the given sessions were created, so
// no session has a raw CSRF token in memory. That is the case in which
// EnsureCSRFPersisted mints a CSRF token and journals a fresh hash, which is
// what an unauthorized session must never get (#1266).
func restartedLegacyPanelState(t *testing.T, users []User, create func(*SessionRegistry) []Session) (*managementState, []Session) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	before, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	sessions := create(before)
	after, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	return &managementState{statePath: statePath, sessions: after, users: users}, sessions
}

// journaledCSRFUpserts counts journal upserts for token that carry a CSRF
// hash other than original.
func journaledCSRFUpserts(t *testing.T, state *managementState, token, original string) int {
	t.Helper()
	body, err := os.ReadFile(persistedSessionStorePath(state.statePath) + ".journal")
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := hashSessionSecret(token)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var record sessionJournalRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode journal line %q: %v", line, err)
		}
		if record.Operation == "upsert" && record.TokenHash == tokenHash && record.Session != nil && record.Session.CSRFHash != original {
			count++
		}
	}
	return count
}

func storedCSRFHash(t *testing.T, registry *SessionRegistry, token string) string {
	t.Helper()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record, ok := registry.sessions[hashSessionSecret(token)]
	if !ok {
		t.Fatal("session missing from registry")
	}
	return record.CSRFHash
}

func assertLegacyLoginPage(t *testing.T, state *managementState, token string) {
	t.Helper()
	original := storedCSRFHash(t, state.sessionRegistry(), token)
	body := legacyPanelFor(t, state, token)
	if !strings.Contains(body, legacyLoginMarker(t)) {
		t.Fatalf("legacy panel did not render the login page for an unauthorized session")
	}
	if _, ok := state.sessionRegistry().Get(token); ok {
		t.Fatal("unauthorized session was not revoked by the legacy panel")
	}
	if n := journaledCSRFUpserts(t, state, token, original); n != 0 {
		t.Fatalf("legacy panel journaled %d fresh CSRF hash(es) for an unauthorized session", n)
	}
}

// The legacy panel is the call-site twin of /api/auth/status (#1336): a
// deleted user's session must get the login page, be revoked, and not have a
// CSRF hash minted and persisted for it (#1266).
func TestLegacyPanelRevokesSessionOfDeletedUser(t *testing.T) {
	users := []User{{Username: "alice", Role: "admin", Locale: "en"}, {Username: "bob", Role: "admin", Locale: "en"}}
	state, sessions := restartedLegacyPanelState(t, users, func(r *SessionRegistry) []Session {
		return []Session{mustCreateSession(t, r, "alice", "admin"), mustCreateSession(t, r, "bob", "admin")}
	})
	live, orphan := sessions[0], sessions[1]

	original := storedCSRFHash(t, state.sessionRegistry(), live.Token)
	if body := legacyPanelFor(t, state, live.Token); strings.Contains(body, legacyLoginMarker(t)) {
		t.Fatal("a live user's session should get the authenticated panel")
	}
	if _, ok := state.sessionRegistry().Get(live.Token); !ok {
		t.Fatal("a live user's session must survive the legacy panel")
	}
	// Control: after a restart the authorized session does get a fresh,
	// journaled CSRF hash, so the zero-count assertion below is not vacuous.
	if n := journaledCSRFUpserts(t, state, live.Token, original); n != 1 {
		t.Fatalf("authorized session after restart: journaled CSRF upserts=%d, want 1", n)
	}

	state.mu.Lock()
	state.users = state.users[:1] // delete bob
	state.mu.Unlock()
	assertLegacyLoginPage(t, state, orphan.Token)
}

// A session minted before the owner armed a second factor lacks the
// second-factor mark; the middleware revokes it, so the legacy panel must too.
func TestLegacyPanelRevokesFactorDeficientSession(t *testing.T) {
	users := []User{{Username: "alice", Role: "admin", Locale: "en", TOTPEnabled: true}}
	state, sessions := restartedLegacyPanelState(t, users, func(r *SessionRegistry) []Session {
		return []Session{mustCreateSession(t, r, "alice", "admin")}
	})
	assertLegacyLoginPage(t, state, sessions[0].Token)
}

// A NaivePassword-fallback bootstrap session is valid only while no user row
// exists and the instance was never provisioned (#1112). Once a real account
// exists it is an orphan like any other and the legacy panel must revoke it.
func TestLegacyPanelRevokesStaleBootstrapSession(t *testing.T) {
	users := []User{{Username: "alice", Role: "admin", Locale: "en"}}
	state, sessions := restartedLegacyPanelState(t, users, func(r *SessionRegistry) []Session {
		session, err := r.Create(SessionCreateInput{Username: "admin", Role: "admin", Bootstrap: true})
		if err != nil {
			t.Fatal(err)
		}
		return []Session{session}
	})
	state.settings.NaivePassword = "fallback-secret"
	assertLegacyLoginPage(t, state, sessions[0].Token)
}
