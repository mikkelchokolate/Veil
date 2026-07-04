package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	forbidden := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	forbidden = forbidden.WithContext(context.WithValue(forbidden.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleUsersRoute(rec, forbidden)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", rec.Code)
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

	put := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"admin","locale":"ru"}`))
	put.Header.Set("Content-Type", "application/json")
	put = put.WithContext(context.WithValue(put.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleUserByNameRoute(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUserByNameRoute(rec, deleteReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}

	lastAdmin := httptest.NewRequest(http.MethodDelete, "/api/users/alice", nil)
	lastAdmin = lastAdmin.WithContext(context.WithValue(lastAdmin.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUserByNameRoute(rec, lastAdmin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("last admin DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}

	notFound := httptest.NewRequest(http.MethodGet, "/api/users/bob", nil)
	notFound = notFound.WithContext(context.WithValue(notFound.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleUserByNameRoute(rec, notFound)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}
}

func TestHandleAuthSessions(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	adminSession, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	viewerSession, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	state := &managementState{sessions: registry}

	list := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	list.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec := httptest.NewRecorder()
	state.handleAuthSessions(rec, list)
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
	state.handleAuthSessions(rec, deleteReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	missingID := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions", strings.NewReader(`{"id":" "}`))
	missingID.Header.Set("Content-Type", "application/json")
	missingID.AddCookie(&http.Cookie{Name: "veil_session", Value: adminSession.Token})
	rec = httptest.NewRecorder()
	state.handleAuthSessions(rec, missingID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id status=%d body=%s", rec.Code, rec.Body.String())
	}

	viewer := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	viewer.AddCookie(&http.Cookie{Name: "veil_session", Value: viewerSession.Token})
	rec = httptest.NewRecorder()
	state.handleAuthSessions(rec, viewer)
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

func TestClientIPRespectsXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", " 10.0.0.1 , 10.0.0.2")
	if got := clientIP(req); got != "10.0.0.1" {
		t.Fatalf("clientIP=%q", got)
	}
}
