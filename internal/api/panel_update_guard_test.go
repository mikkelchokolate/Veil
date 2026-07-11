package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelUpdateRejectsConcurrentUpdate(t *testing.T) {
	state := &managementState{}
	state.updateMu.Lock()
	defer state.updateMu.Unlock()

	response := httptest.NewRecorder()
	if state.beginPanelUpdate(response) {
		t.Fatal("concurrent update unexpectedly acquired the update lock")
	}
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "another panel update is already in progress") {
		t.Fatalf("unexpected conflict response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPanelUpdateRuntimeConflictReleasesUpdateLock(t *testing.T) {
	state := &managementState{}
	state.serviceActionMu.Lock()
	defer state.serviceActionMu.Unlock()

	response := httptest.NewRecorder()
	if state.beginPanelUpdate(response) {
		t.Fatal("update unexpectedly acquired a busy runtime lock")
	}
	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
	}
	if !state.updateMu.TryLock() {
		t.Fatal("failed panel update leaked the update lock")
	}
	state.updateMu.Unlock()
}

func TestPanelUpdateGuardReleasesBothLocks(t *testing.T) {
	state := &managementState{}
	response := httptest.NewRecorder()
	if !state.beginPanelUpdate(response) {
		t.Fatalf("failed to acquire panel update guard: status=%d body=%s", response.Code, response.Body.String())
	}
	if state.updateMu.TryLock() {
		state.updateMu.Unlock()
		t.Fatal("update lock was not held")
	}
	if state.serviceActionMu.TryLock() {
		state.serviceActionMu.Unlock()
		t.Fatal("runtime lock was not held")
	}

	state.endPanelUpdate()

	if !state.updateMu.TryLock() {
		t.Fatal("update lock was not released")
	}
	state.updateMu.Unlock()
	if !state.serviceActionMu.TryLock() {
		t.Fatal("runtime lock was not released")
	}
	state.serviceActionMu.Unlock()
}
