package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestSetupCompleteRetriesAfterAFailedSave locks #1441: the provisioning latch
// (in-memory usersEverExisted plus the durable users-provisioned.marker) used
// to be taken BEFORE the state save. When the save failed the handler rolled
// the users back but left the latch, so the instance answered "first-run setup
// is already complete" (409) forever with zero users, and only the host CLI
// could recover it.
func TestSetupCompleteRetriesAfterAFailedSave(t *testing.T) {
	state := newTestSetupState(t, true)
	// Any saveLocked failure works: with required apply tracking and no apply
	// subsystem the save refuses to persist an unversioned state.
	state.requireApplyTracking = true

	failed := httptest.NewRecorder()
	state.handleSetupComplete(failed, newSetupCompleteRequest())
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("setup with a failing save: status=%d body=%s", failed.Code, failed.Body.String())
	}
	if len(state.users) != 0 || state.setup.Completed {
		t.Fatalf("failed setup was not rolled back: users=%+v setup=%+v", state.users, state.setup)
	}
	if _, err := os.Stat(state.usersProvisionedMarkerPath()); !os.IsNotExist(err) {
		t.Fatalf("failed setup left the provisioning marker behind: %v", err)
	}

	// The transient cause disappears; the same instance has to allow a retry.
	state.requireApplyTracking = false
	retried := httptest.NewRecorder()
	state.handleSetupComplete(retried, newSetupCompleteRequest())
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry after a failed save: status=%d body=%s", retried.Code, retried.Body.String())
	}
	if len(state.users) != 1 || !state.setup.Completed {
		t.Fatalf("retry did not create the admin: users=%+v setup=%+v", state.users, state.setup)
	}

	// A restart on the same data directory must see the completed setup, not a
	// provisioned-with-zero-users lockdown.
	restarted := &managementState{
		statePath:      state.statePath,
		applyRoot:      state.applyRoot,
		setupAllowed:   true,
		passwordHasher: bcryptPasswordHasher{cost: bcrypt.MinCost},
		settings:       state.settings,
	}
	status := httptest.NewRecorder()
	restarted.handleSetupStatus(status, httptest.NewRequest(http.MethodGet, "/api/setup/status", nil))
	var response SetupStatusResponse
	if err := json.NewDecoder(status.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Required {
		t.Fatalf("setup still required after a successful retry: %+v", response)
	}
	conflict := httptest.NewRecorder()
	restarted.handleSetupComplete(conflict, newSetupCompleteRequest())
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second setup on the restarted instance: status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}
