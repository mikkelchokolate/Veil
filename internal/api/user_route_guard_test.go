package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func adminUserRouteRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
}

func userRouteTestState(t *testing.T) *managementState {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	return &managementState{
		sessions: registry,
		users: []User{{
			Username:     "alice",
			PasswordHash: "hash",
			Role:         "admin",
			Locale:       "en",
		}},
	}
}

func TestUserRouteGuardMapsLastAdministratorDemotionToBadRequest(t *testing.T) {
	state := userRouteTestState(t)
	req := adminUserRouteRequest(http.MethodPut, "/api/users/alice", `{"role":"viewer","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cannot remove the last administrator") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if state.users[0].Role != "admin" {
		t.Fatalf("last administrator was demoted: %+v", state.users[0])
	}
}

func TestUserRouteGuardPreservesUnrelatedValidationResponses(t *testing.T) {
	state := userRouteTestState(t)
	req := adminUserRouteRequest(http.MethodPut, "/api/users/alice", `{"role":"owner","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "valid role") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestUserRouteGuardRejectsCollectionOperationsOnNamedPaths(t *testing.T) {
	for _, tc := range []struct {
		method string
		body   string
	}{
		{method: http.MethodGet},
		{method: http.MethodPost, body: `{"username":"bob","password":"secret","role":"viewer"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			state := userRouteTestState(t)
			req := adminUserRouteRequest(tc.method, "/api/users/alice", tc.body)
			rec := httptest.NewRecorder()

			state.handleUsersRouteWithAdminInvariant(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if allow := rec.Header().Get("Allow"); allow != "PUT, DELETE" {
				t.Fatalf("Allow = %q", allow)
			}
			if len(state.users) != 1 {
				t.Fatalf("named collection request mutated users: %+v", state.users)
			}
		})
	}
}

func TestUserRouteGuardRejectsNestedUserPaths(t *testing.T) {
	state := userRouteTestState(t)
	req := adminUserRouteRequest(http.MethodDelete, "/api/users/alice/sessions", "")
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(state.users) != 1 {
		t.Fatalf("nested path mutated users: %+v", state.users)
	}
}

func TestUserRouteGuardRejectsUnsafeCreateUsername(t *testing.T) {
	state := userRouteTestState(t)
	req := adminUserRouteRequest(http.MethodPost, "/api/users", `{"username":"bad/name","password":"secret","role":"viewer","locale":"en"}`)
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "username must be 3-64 characters") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if len(state.users) != 1 {
		t.Fatalf("unsafe username was created: %+v", state.users)
	}
}

func TestUserRouteGuardPreservesUnsupportedMediaTypeResponse(t *testing.T) {
	state := userRouteTestState(t)
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"bad/name"}`))
	req.Header.Set("Content-Type", "text/plain")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
