package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHandleUsersRouteAdminOperations(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		users: []User{{
			Username:     "alice",
			PasswordHash: "",
			Role:         "admin",
		}},
	}

	get := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	get = get.WithContext(context.WithValue(get.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleUsersRoute(rec, get)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"bob","password":"a-long-password","role":"viewer"}`))
	post.Header.Set("Content-Type", "application/json")
	post = post.WithContext(context.WithValue(post.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUsersRoute(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	// A bare 201 is not enough: the response must echo the created user and
	// the state must actually contain bob as a viewer (#838).
	var created struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Locale   string `json:"locale"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("create body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if created.Username != "bob" || created.Role != "viewer" || created.Locale != "en" {
		t.Fatalf("create body wrong: %+v", created)
	}
	bobFound := false
	for _, user := range state.users {
		if user.Username == "bob" {
			bobFound = true
			if user.Role != "viewer" || user.PasswordHash == "" {
				t.Fatalf("created user missing role/hash: %+v", user)
			}
		}
	}
	if !bobFound {
		t.Fatal("POST returned 201 but bob is not in state")
	}

	forbidden := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	forbidden = forbidden.WithContext(context.WithValue(forbidden.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleUsersRoute(rec, forbidden)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", rec.Code)
	}

	// Viewer must also be denied on the mutation path, not just reads.
	viewerPost := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"mallory","password":"a-long-password","role":"admin"}`))
	viewerPost.Header.Set("Content-Type", "application/json")
	viewerPost = viewerPost.WithContext(context.WithValue(viewerPost.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleUsersRoute(rec, viewerPost)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "mallory" {
			t.Fatal("viewer POST created a user despite 403")
		}
	}
}

func TestHandleUsersRouteRejectsInvalidRoleAndLocale(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	state := &managementState{sessions: registry, users: []User{}}

	cases := []struct {
		name string
		body string
	}{
		{"missing role", `{"username":"bob","password":"a-long-password"}`},
		{"invalid role", `{"username":"bob","password":"a-long-password","role":"superuser"}`},
		{"invalid locale", `{"username":"bob","password":"a-long-password","role":"viewer","locale":"de"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
			rec := httptest.NewRecorder()
			state.handleUsersRoute(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleUserByNameRoute(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "alice", PasswordHash: string(hash), Role: "admin"},
			{Username: "bob", PasswordHash: string(hash), Role: "viewer"},
		},
	}

	// Exercise the production item route (handleReliableUserItemRoute →
	// handleAtomicUserUpdate / handleAtomicUserDelete), not the legacy
	// handleUserByNameRoute the mux no longer serves (#838).
	put := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"admin","locale":"ru"}`))
	put.Header.Set("Content-Type", "application/json")
	put = put.WithContext(context.WithValue(put.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	// The update response carries the user golden, not just a 200.
	var updated struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Locale   string `json:"locale"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("PUT body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if updated.Username != "alice" || updated.Role != "admin" || updated.Locale != "ru" {
		t.Fatalf("PUT body wrong: %+v", updated)
	}
	for _, user := range state.users {
		if user.Username == "alice" && (user.Role != "admin" || user.Locale != "ru") {
			t.Fatalf("state not updated by PUT: %+v", user)
		}
	}

	// A viewer must be denied before the mutation runs.
	viewerPut := httptest.NewRequest(http.MethodPut, "/api/users/bob", strings.NewReader(`{"role":"admin"}`))
	viewerPut.Header.Set("Content-Type", "application/json")
	viewerPut = viewerPut.WithContext(context.WithValue(viewerPut.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, viewerPut)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "bob" && user.Role != "viewer" {
			t.Fatalf("viewer PUT mutated bob: %+v", user)
		}
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, deleteReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, user := range state.users {
		if user.Username == "bob" {
			t.Fatal("bob still present after 204 delete")
		}
	}

	viewerDelete := httptest.NewRequest(http.MethodDelete, "/api/users/alice", nil)
	viewerDelete = viewerDelete.WithContext(context.WithValue(viewerDelete.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, viewerDelete)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}

	lastAdmin := httptest.NewRequest(http.MethodDelete, "/api/users/alice", nil)
	lastAdmin = lastAdmin.WithContext(context.WithValue(lastAdmin.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, lastAdmin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("last admin DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}

	notFound := httptest.NewRequest(http.MethodGet, "/api/users/bob", nil)
	notFound = notFound.WithContext(context.WithValue(notFound.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleReliableUserItemRoute(rec, notFound)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}
}

func TestHandleAuthSessions(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	adminSession, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	viewerSession, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	state := &managementState{sessions: registry}

	// Exercise the production handler the mux registers
	// (handlePersistentAuthSessions), not the legacy in-memory-only path —
	// revoke must persist and the session must actually be gone (#827).
	list := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	list.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec := httptest.NewRecorder()
	state.handlePersistentAuthSessions(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), adminSession.ID) || !strings.Contains(rec.Body.String(), viewerSession.ID) {
		t.Fatalf("expected both sessions in list, got %s", rec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":"`+viewerSession.ID+`"}`))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteReq.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec = httptest.NewRecorder()
	state.handlePersistentAuthSessions(rec, deleteReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("delete body missing success=true: %s", rec.Body.String())
	}
	// The revoked session must be gone from the registry — a StatusOK without
	// the deletion is a false green.
	if _, ok := registry.Get(viewerSession.Token); ok {
		t.Fatal("revoked viewer session is still in the registry")
	}

	missingID := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":" "}`))
	missingID.Header.Set("Content-Type", "application/json")
	missingID.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec = httptest.NewRecorder()
	state.handlePersistentAuthSessions(rec, missingID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Deleting an unknown id must surface 404, not a soft 200.
	unknownID := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":"no-such-session"}`))
	unknownID.Header.Set("Content-Type", "application/json")
	unknownID.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec = httptest.NewRecorder()
	state.handlePersistentAuthSessions(rec, unknownID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d body=%s", rec.Code, rec.Body.String())
	}

	viewer := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	viewer.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSession.Token})
	rec = httptest.NewRecorder()
	state.handlePersistentAuthSessions(rec, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", rec.Code)
	}
}

func TestHandleAuthStatusUnauthenticated(t *testing.T) {
	state := &managementState{sessions: mustNewSessionRegistry("")}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	state.handleAuthStatus(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthLocaleRequiresSession(t *testing.T) {
	state := &managementState{sessions: mustNewSessionRegistry("")}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/locale", strings.NewReader(`{"locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	state.handleAuthLocale(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequestHasAdminRoleFallsBackToCookie(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	adminSession, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	viewerSession, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	state := &managementState{sessions: registry}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	adminReq.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	if !requestHasAdminRole(state, adminReq) {
		t.Fatal("expected admin request")
	}

	viewerReq := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	viewerReq.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSession.Token})
	if requestHasAdminRole(state, viewerReq) {
		t.Fatal("expected non-admin request")
	}
}

func TestClientIPIgnoresXForwardedForFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", " 10.0.0.1 , 10.0.0.2")
	if got := clientIP(req); got != "192.0.2.1" {
		t.Fatalf("clientIP=%q", got)
	}
}

func TestHandleAuthStatusMethodsAndCSRFPaths(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	session, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	state := &managementState{sessions: registry}

	post := httptest.NewRequest(http.MethodPost, "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	state.handleAuthStatus(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", rec.Code)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	get.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec = httptest.NewRecorder()
	state.handleAuthStatus(rec, get)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":true`) {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Force CSRF regeneration to fail by emptying the raw CSRF store and making random generation fail.
	tokenHash := hashSessionSecret(session.Token)
	registry.mu.Lock()
	registry.rawCSRF[tokenHash] = ""
	registry.mu.Unlock()
	old := randomReader
	randomReader = func(b []byte) (int, error) { return 0, errors.New("random failure") }
	t.Cleanup(func() { randomReader = old })
	get = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	get.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec = httptest.NewRecorder()
	state.handleAuthStatus(rec, get)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("csrf error status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthLocaleValidation(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	session, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	state := newManagementState(ServerInfo{Mode: "dev", SetupAllowed: true})
	state.sessions = registry
	state.users = []User{{Username: "alice", Role: "admin", Locale: "en"}}

	cases := []struct {
		name       string
		method     string
		body       string
		cookie     bool
		ctxUser    string
		wantStatus int
		setup      func()
	}{
		{"method not allowed", http.MethodGet, `{"locale":"ru"}`, true, "", http.StatusMethodNotAllowed, nil},
		{"no cookie", http.MethodPost, `{"locale":"ru"}`, false, "", http.StatusUnauthorized, nil},
		{"session mismatch", http.MethodPost, `{"locale":"ru"}`, true, "bob", http.StatusForbidden, nil},
		{"invalid locale", http.MethodPost, `{"locale":"de"}`, true, "", http.StatusBadRequest, nil},
		{"user not found", http.MethodPost, `{"locale":"ru"}`, true, "", http.StatusNotFound, func() { state.users = []User{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
				defer func() { state.users = []User{{Username: "alice", Role: "admin", Locale: "en"}} }()
			}
			req := httptest.NewRequest(tc.method, "/api/auth/locale", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
			}
			if tc.ctxUser != "" {
				req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, tc.ctxUser))
			}
			rec := httptest.NewRecorder()
			state.handleAuthLocale(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleAuthLocalePersistsLocale(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	session, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	state := newManagementState(ServerInfo{Mode: "dev", StatePath: filepath.Join(t.TempDir(), "state.json")})
	state.sessions = registry
	state.users = []User{{Username: "alice", Role: "admin", Locale: "en"}}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/locale", strings.NewReader(`{"locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()
	state.handleAuthLocale(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"locale":"ru"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLoginValidation(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	registry, _ := NewSessionRegistry("")
	now := time.Now()
	state := &managementState{
		sessions:        registry,
		users:           []User{{Username: "alice", PasswordHash: string(hash), Role: "admin"}},
		settings:        Settings{NaivePassword: "naive-password", PanelAccess: "local"},
		loginBackoffNow: func() time.Time { return now },
	}

	get := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	state.handleLogin(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}

	badJSON := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{`))
	badJSON.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	state.handleLogin(rec, badJSON)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status=%d", rec.Code)
	}

	badCreds := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"wrong"}`))
	badCreds.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	state.handleLogin(rec, badCreds)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad creds status=%d body=%s", rec.Code, rec.Body.String())
	}
	now = now.Add(2 * time.Second)

	old := randomReader
	randomReader = func(b []byte) (int, error) { return 0, errors.New("random failure") }
	t.Cleanup(func() { randomReader = old })
	valid := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret-password"}`))
	valid.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	state.handleLogin(rec, valid)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("session failure status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLoginFallbackAdmin(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		users:    []User{},
		settings: Settings{NaivePassword: "naive-password", PanelAccess: "local"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"naive-password"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	state.handleLogin(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Even the legacy handler owes the session contract: a bare 200 without
	// the cookie/csrfToken pair is not a usable login (#827).
	assertLoginSetsSessionCookieAndCSRF(t, rec)
}

func TestHandleLogout(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	session, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	state := &managementState{sessions: registry, settings: Settings{PanelAccess: "local"}}

	get := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	state.handleLogout(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	post.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec = httptest.NewRecorder()
	state.handleLogout(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Logout must actually expire the cookie: empty value + negative MaxAge
	// (#827) — a 200 without the clear-cookie header leaves the browser
	// holding a dead session.
	expired := sessionSetCookie(rec)
	if expired == nil || expired.Value != "" || expired.MaxAge >= 0 {
		t.Fatalf("logout did not emit an expiring veil_session cookie: %+v", expired)
	}
	if _, ok := registry.Get(session.Token); ok {
		t.Fatal("session was not revoked")
	}
}

func TestHandleAuthSessionsMethodNotAllowed(t *testing.T) {
	state := &managementState{sessions: mustNewSessionRegistry("")}
	put := httptest.NewRequest(http.MethodPut, "/api/auth/sessions", nil)
	put = put.WithContext(context.WithValue(put.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleAuthSessions(rec, put)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHandleUsersRouteMethodNotAllowed(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	state := &managementState{sessions: registry, users: []User{}}
	put := httptest.NewRequest(http.MethodPut, "/api/users", nil)
	put = put.WithContext(context.WithValue(put.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleUsersRoute(rec, put)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHandleUserByNameRouteValidation(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	state := &managementState{
		sessions: registry,
		users: []User{
			{Username: "alice", PasswordHash: string(hash), Role: "admin"},
		},
	}

	invalidPath := httptest.NewRequest(http.MethodPut, "/api/users", nil)
	invalidPath = invalidPath.WithContext(context.WithValue(invalidPath.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleUserByNameRoute(rec, invalidPath)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("invalid path status=%d", rec.Code)
	}

	badRole := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"superuser"}`))
	badRole.Header.Set("Content-Type", "application/json")
	badRole = badRole.WithContext(context.WithValue(badRole.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUserByNameRoute(rec, badRole)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad role status=%d body=%s", rec.Code, rec.Body.String())
	}

	badLocale := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"admin","locale":"de"}`))
	badLocale.Header.Set("Content-Type", "application/json")
	badLocale = badLocale.WithContext(context.WithValue(badLocale.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUserByNameRoute(rec, badLocale)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad locale status=%d body=%s", rec.Code, rec.Body.String())
	}
}
