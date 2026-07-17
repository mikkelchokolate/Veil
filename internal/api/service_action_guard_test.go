package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceActionRejectsConcurrentMutation(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test"})
	state.serviceActionMu.Lock()
	defer state.serviceActionMu.Unlock()

	request := httptest.NewRequest(http.MethodPost, "/api/services/caddy/restart", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	state.handleServiceActionRoute(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "another service action is already in progress") {
		t.Fatalf("unexpected conflict response: %s", response.Body.String())
	}
}
