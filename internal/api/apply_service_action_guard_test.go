package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRequiresServiceActionLockOnlyForRuntimeMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ApplyRequest
		want bool
	}{
		{name: "stage only", req: ApplyRequest{Confirm: true}, want: false},
		{name: "live promotion", req: ApplyRequest{Confirm: true, ApplyLive: true}, want: true},
		{name: "service reload", req: ApplyRequest{Confirm: true, ApplyServices: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyRequiresServiceActionLock(tc.req); got != tc.want {
				t.Fatalf("applyRequiresServiceActionLock(%+v) = %t, want %t", tc.req, got, tc.want)
			}
		})
	}
}

func TestLiveApplyRejectsConcurrentServiceAction(t *testing.T) {
	dir := t.TempDir()
	state := newManagementState(ServerInfo{
		Version:   "test",
		StatePath: filepath.Join(dir, "state.json"),
		ApplyRoot: filepath.Join(dir, "etc"),
	})
	state.serviceActionMu.Lock()
	defer state.serviceActionMu.Unlock()

	request := httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	state.handleApply(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "another service action is already in progress") {
		t.Fatalf("unexpected conflict response: %s", response.Body.String())
	}
}
