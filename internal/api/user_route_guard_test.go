package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserRouteGuardMapsLastAdministratorDemotionToBadRequest(t *testing.T) {
	registry, _ := NewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		users: []User{{
			Username:     "alice",
			PasswordHash: "hash",
			Role:         "admin",
			Locale:       "en",
		}},
	}

	req := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"viewer","locale":"en"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
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
	registry, _ := NewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		users: []User{{
			Username:     "alice",
			PasswordHash: "hash",
			Role:         "admin",
			Locale:       "en",
		}},
	}

	req := httptest.NewRequest(http.MethodPut, "/api/users/alice", strings.NewReader(`{"role":"owner","locale":"en"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()

	state.handleUsersRouteWithAdminInvariant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "valid role") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
