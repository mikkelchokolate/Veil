package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUsersCRUDDeniesViewerThroughMiddleware drives the production route
// registrations behind the real auth middleware: a live viewer session must
// get 403 with an error body on every users mutation and listing, and the
// stored user set must remain untouched (#838).
func TestUsersCRUDDeniesViewerThroughMiddleware(t *testing.T) {
	state := &managementState{
		sessions: mustNewSessionRegistry(""),
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "viewer", PasswordHash: "viewer-hash", Role: "viewer", Locale: "en"},
		},
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	mux := http.NewServeMux()
	state.register(mux)
	router := authMiddleware(state, "", mux)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/users", ""},
		{http.MethodPost, "/api/users", `{"username":"mallory","password":"long-enough-password","role":"viewer"}`},
		{http.MethodPut, "/api/users/viewer", `{"role":"admin","locale":"en"}`},
		{http.MethodDelete, "/api/users/viewer", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
			req.Header.Set("X-CSRF-Token", viewer.CSRFToken)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("viewer %s %s = %d, want 403; body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if msg := responseErrorMessage(t, rec.Body.Bytes()); msg == "" {
				t.Fatalf("403 response carries no error message: %s", rec.Body.String())
			}
		})
	}

	// Nothing was mutated or exposed.
	if len(state.users) != 2 {
		t.Fatalf("viewer request mutated users: %+v", state.users)
	}
	if state.users[1].Role != "viewer" {
		t.Fatalf("viewer escalated own role: %+v", state.users[1])
	}
	// The viewer session must still be valid (denied request ≠ revoked).
	if _, ok := state.sessionRegistry().Get(viewer.Token); !ok {
		t.Fatal("viewer session was revoked by a denied request")
	}
}
