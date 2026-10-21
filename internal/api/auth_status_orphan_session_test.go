package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// authStatusFor calls the persistent status handler with the given session
// cookie and decodes the body.
func authStatusFor(t *testing.T, state *managementState, token string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: token})
	rec := httptest.NewRecorder()
	state.handlePersistentAuthStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body
}

func orphanStatusTestState(t *testing.T, users []User) *managementState {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	return &managementState{
		statePath: filepath.Join(t.TempDir(), "state.json"),
		sessions:  registry,
		users:     users,
	}
}

// A session whose user was deleted must not be reported as authenticated:
// the API middleware revokes it on the next protected call, so the status
// endpoint must agree — and must not mint/persist a CSRF hash for it (#1266).
func TestAuthStatusRevokesSessionOfDeletedUser(t *testing.T) {
	state := orphanStatusTestState(t, []User{{Username: "alice", Role: "admin"}, {Username: "bob", Role: "admin"}})
	sess := mustCreateSession(t, state.sessionRegistry(), "bob", "admin")

	if body := authStatusFor(t, state, sess.Token); body["authenticated"] != true {
		t.Fatalf("live user's session should be authenticated: %v", body)
	}

	state.mu.Lock()
	state.users = state.users[:1] // delete bob
	state.mu.Unlock()

	body := authStatusFor(t, state, sess.Token)
	if body["authenticated"] != false {
		t.Fatalf("deleted user's session reported authenticated: %v", body)
	}
	if _, ok := body["csrfToken"]; ok {
		t.Fatalf("deleted user's session was issued a CSRF token: %v", body)
	}
	if _, ok := state.sessionRegistry().Get(sess.Token); ok {
		t.Fatal("deleted user's session was not revoked by the status endpoint")
	}
}

// The status endpoint reports the user row's current role (what the
// middleware authorizes with), not the role frozen into the session.
func TestAuthStatusReportsCurrentRoleNotSessionRole(t *testing.T) {
	state := orphanStatusTestState(t, []User{{Username: "alice", Role: "viewer"}})
	sess := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	body := authStatusFor(t, state, sess.Token)
	if body["authenticated"] != true || body["role"] != "viewer" {
		t.Fatalf("status should report the current role viewer: %v", body)
	}
}

// A bootstrap (NaivePassword fallback) session stays valid while the
// fallback precondition holds and is revoked once a real account exists,
// matching authorizeCookieSession (#1112).
func TestAuthStatusBootstrapSessionFollowsMiddlewareRule(t *testing.T) {
	state := orphanStatusTestState(t, nil)
	state.settings.NaivePassword = "fallback-secret"
	sess, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin", Bootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	if body := authStatusFor(t, state, sess.Token); body["authenticated"] != true {
		t.Fatalf("valid bootstrap session should be authenticated: %v", body)
	}

	state.mu.Lock()
	state.users = []User{{Username: "alice", Role: "admin"}}
	state.mu.Unlock()
	if body := authStatusFor(t, state, sess.Token); body["authenticated"] != false {
		t.Fatalf("bootstrap session must be revoked once a real user exists: %v", body)
	}
	if _, ok := state.sessionRegistry().Get(sess.Token); ok {
		t.Fatal("bootstrap session still in the registry after a real user exists")
	}
}

// The other two bootstrap preconditions: zero users but the instance was
// provisioned before (users later all deleted), and no fallback configured.
// Either one revokes the bootstrap session on the status endpoint (#1112).
func TestAuthStatusBootstrapSessionRevokedWhenProvisionedOrFallbackOff(t *testing.T) {
	for name, mutate := range map[string]func(*managementState){
		"previously provisioned":  func(s *managementState) { s.usersEverExisted = true },
		"fallback not configured": func(s *managementState) { s.settings.NaivePassword = "" },
	} {
		t.Run(name, func(t *testing.T) {
			state := orphanStatusTestState(t, nil)
			state.settings.NaivePassword = "fallback-secret"
			sess, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin", Bootstrap: true})
			if err != nil {
				t.Fatal(err)
			}
			state.mu.Lock()
			mutate(state)
			state.mu.Unlock()
			if body := authStatusFor(t, state, sess.Token); body["authenticated"] != false {
				t.Fatalf("bootstrap session must not authenticate: %v", body)
			}
			if _, ok := state.sessionRegistry().Get(sess.Token); ok {
				t.Fatal("bootstrap session still in the registry")
			}
		})
	}
}
