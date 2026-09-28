package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// authedV1Request issues an authenticated panel request: once users exist the
// dev-anonymous bypass is disabled, so security regression tests must log in.
// The session username must exist in state.users — the middleware revokes
// sessions whose account was deleted — and mutating requests need CSRF.
func authedV1Request(t *testing.T, state *managementState, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	session, err := state.sessions.Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestRollbackPreservesCurrentPanelUsersAndSetup locks #1094: a rollback to a
// historical snapshot must never resurrect deleted panel accounts, roll
// password hashes backwards, demote administrators, or undo completed setup.
func TestRollbackPreservesCurrentPanelUsersAndSetup(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	// The selected revision carries the pre-rotation password hash, a second
	// administrator that is about to be deleted, and an incomplete setup.
	state.mu.Lock()
	state.users = []User{
		{Username: "admin", PasswordHash: "old-hash", Role: "admin"},
		{Username: "dead-admin", PasswordHash: "dead-hash", Role: "admin"},
	}
	state.setup = SetupState{Completed: false}
	state.mu.Unlock()

	create := authedV1Request(t, state, router, http.MethodPost, "/api/v1/clients", `{"name":"rev1-client","enabled":true}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", create.Code, create.Body.String())
	}
	first, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}

	// Post-snapshot security changes: rotated admin password, a deleted
	// administrator, and a completed setup. None may be undone by the
	// rollback.
	state.mu.Lock()
	state.users = []User{{Username: "admin", PasswordHash: "new-hash", Role: "admin"}}
	state.setup = SetupState{Completed: true, CompletedAt: "2024-01-01T00:00:00Z"}
	state.mu.Unlock()

	created := unwrapClient(t, create.Body.Bytes())
	patch := authedV1Request(t, state, router, http.MethodPatch, "/api/v1/clients/"+created["id"].(string), `{"version":1,"name":"rev2-client"}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch client: %d %s", patch.Code, patch.Body.String())
	}

	rollback := authedV1Request(t, state, router, http.MethodPost, "/api/apply/rollback",
		`{"selectedRevision":`+strconv.FormatUint(first.Desired, 10)+`,"confirm":true}`)
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
	}

	state.mu.Lock()
	users := append([]User(nil), state.users...)
	setup := state.setup
	state.mu.Unlock()
	if len(users) != 1 || users[0].Username != "admin" || users[0].PasswordHash != "new-hash" {
		t.Fatalf("rollback resurrected or rewrote panel users: %+v", users)
	}
	if !setup.Completed {
		t.Fatal("rollback un-completed panel setup")
	}

	// The immutable payload recorded for the new revision must carry the
	// grafted auth state too — a later render of it must not resurrect the
	// old administrator.
	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(after.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var stored managementSnapshot
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Users) != 1 || stored.Users[0].Username != "admin" || stored.Users[0].PasswordHash != "new-hash" {
		t.Fatalf("stored revision snapshot carries stale users: %+v", stored.Users)
	}
	if !stored.Setup.Completed {
		t.Fatal("stored revision snapshot un-completed panel setup")
	}
}

// TestRollbackKeepsCredentialRevocationsMonotonic locks #1099 end to end:
// rotating a credential and disabling a client after the snapshot must
// survive a rollback — the old credential's revoked tombstone stays, the
// rotated credential stays active, a deleted client stays deleted, and a
// disabled client stays disabled while its configuration still rolls back.
func TestRollbackKeepsCredentialRevocationsMonotonic(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"rb-in","protocol":"hysteria2","transport":"udp","port":14450,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	create := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"alice","enabled":true}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create alice: %d %s", create.Code, create.Body.String())
	}
	aliceID := unwrapClient(t, create.Body.Bytes())["id"].(string)
	binding := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+aliceID+"/bindings",
		`{"inboundId":"rb-in","credential":"cred-A"}`)
	if binding.Code != http.StatusCreated && binding.Code != http.StatusOK {
		t.Fatalf("create binding: %d %s", binding.Code, binding.Body.String())
	}
	var bindingView struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(binding.Body.Bytes(), &bindingView); err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	bindingID := bindingView.ID

	ghost := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"ghost","enabled":true}`)
	if ghost.Code != http.StatusCreated {
		t.Fatalf("create ghost: %d %s", ghost.Code, ghost.Body.String())
	}
	ghostID := unwrapClient(t, ghost.Body.Bytes())["id"].(string)

	first, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	var oldCredID string
	if err := state.db.QueryRow(`SELECT id FROM client_credentials WHERE binding_id=?`, bindingID).Scan(&oldCredID); err != nil {
		t.Fatalf("load snapshot credential: %v", err)
	}

	// Security events after the snapshot: credential rotation (old value
	// revoked), client rename+disable, and another client's deletion.
	rotate := v1Request(t, router, http.MethodPost,
		"/api/v1/clients/"+aliceID+"/credentials/"+bindingID+"/rotate", `{"value":"cred-B"}`)
	if rotate.Code != http.StatusOK {
		t.Fatalf("rotate credential: %d %s", rotate.Code, rotate.Body.String())
	}
	current := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+aliceID, "")
	if current.Code != http.StatusOK {
		t.Fatalf("get alice: %d %s", current.Code, current.Body.String())
	}
	version := unwrapClient(t, current.Body.Bytes())["version"]
	patch := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+aliceID,
		`{"version":`+strconv.Itoa(int(version.(float64)))+`,"name":"alice-renamed","enabled":false}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("disable alice: %d %s", patch.Code, patch.Body.String())
	}
	deleteGhost := v1Request(t, router, http.MethodDelete, "/api/v1/clients/"+ghostID, "")
	if deleteGhost.Code != http.StatusOK {
		t.Fatalf("delete ghost: %d %s", deleteGhost.Code, deleteGhost.Body.String())
	}

	rollback := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		`{"selectedRevision":`+strconv.FormatUint(first.Desired, 10)+`,"confirm":true}`)
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
	}

	// The revoked credential keeps its tombstone; the rotated credential is
	// still the only active one — never the resurrected snapshot value.
	var oldRevoked sql.NullInt64
	if err := state.db.QueryRow(`SELECT revoked_at FROM client_credentials WHERE id=?`, oldCredID).Scan(&oldRevoked); err != nil {
		t.Fatalf("old credential row: %v", err)
	}
	if !oldRevoked.Valid {
		t.Fatal("rollback un-revoked the rotated-out credential")
	}
	var activeVersion int
	var activeCount int
	if err := state.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(credential_version),0) FROM client_credentials WHERE binding_id=? AND revoked_at IS NULL`, bindingID).
		Scan(&activeCount, &activeVersion); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 || activeVersion != 2 {
		t.Fatalf("active credentials after rollback: count=%d version=%d, want 1 active v2", activeCount, activeVersion)
	}

	// Configuration still rolled back (name restored) but the post-snapshot
	// disable is monotonic and survives.
	restored := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+aliceID, "")
	if restored.Code != http.StatusOK {
		t.Fatalf("get alice: %d %s", restored.Code, restored.Body.String())
	}
	alice := unwrapClient(t, restored.Body.Bytes())
	if alice["name"] != "alice" {
		t.Fatalf("client config was not rolled back: name=%v", alice["name"])
	}
	if enabled, _ := alice["enabled"].(bool); enabled {
		t.Fatal("rollback re-enabled a client disabled after the snapshot")
	}

	// The client deleted after the snapshot stays deleted.
	if got := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+ghostID, ""); got.Code != http.StatusNotFound {
		t.Fatalf("deleted client resurrected by rollback: %d %s", got.Code, got.Body.String())
	}

	// The stored payload for the new revision carries the CURRENT active
	// credential — never the revoked snapshot value — so a render of it cannot
	// serve stale secret material.
	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(after.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var stored managementSnapshot
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	for _, cred := range stored.Credentials {
		if cred.ID == oldCredID {
			t.Fatalf("stored revision snapshot re-includes revoked credential %s", cred.ID)
		}
	}
	if len(stored.Credentials) != 1 || stored.Credentials[0].CredentialVersion != 2 {
		t.Fatalf("stored snapshot credentials = %+v, want only the rotated credential", stored.Credentials)
	}
}
