package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A panic inside the untracked apply workflow must still release s.mu:
// previously the bare s.mu.Unlock() after RunLocked was skipped on panic,
// permanently deadlocking every subsequent management handler (#1137).
func TestUntrackedApplyPanicReleasesStateMutex(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	if state.applyTrackingEnabled() {
		t.Skip("test exercises the untracked apply path")
	}
	old := runApplyWorkflowLocked
	runApplyWorkflowLocked = func(*managementState, ApplyRequest) (ApplyResponse, int, error) {
		panic("workflow exploded")
	}
	t.Cleanup(func() { runApplyWorkflowLocked = old })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("handleApply did not propagate the workflow panic")
			}
		}()
		state.handleApply(httptest.NewRecorder(),
			adminJSONRequest(http.MethodPost, "/api/apply", `{"confirm":true}`))
	}()

	if !state.mu.TryLock() {
		t.Fatal("state mutex is still held after a panicking apply — handlers would deadlock")
	}
	state.mu.Unlock()

	// A subsequent apply must reach the workflow instead of blocking on s.mu.
	runApplyWorkflowLocked = func(*managementState, ApplyRequest) (ApplyResponse, int, error) {
		return ApplyResponse{}, http.StatusOK, nil
	}
	response := httptest.NewRecorder()
	state.handleApply(response,
		adminJSONRequest(http.MethodPost, "/api/apply", `{"confirm":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("apply after panic: status=%d body=%s", response.Code, response.Body.String())
	}
}
