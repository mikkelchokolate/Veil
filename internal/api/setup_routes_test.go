package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

func TestSetupStatusReportsLocalFirstRun(t *testing.T) {
	state := newTestSetupState(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	rec := httptest.NewRecorder()

	state.handleSetupStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response SetupStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.Required || !response.Allowed || response.PanelAccess != "local" {
		t.Fatalf("response=%+v", response)
	}
}

func TestSetupCompleteCreatesFirstAdminOnLocalListener(t *testing.T) {
	state := newTestSetupState(t, true)
	req := newSetupCompleteRequest()
	rec := httptest.NewRecorder()

	state.handleSetupComplete(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !state.setup.Completed || state.setup.CompletedAt == "" || len(state.users) != 1 {
		t.Fatalf("setup=%+v users=%+v", state.setup, state.users)
	}
	if state.users[0].Username != "admin" || state.users[0].Role != "admin" {
		t.Fatalf("user=%+v", state.users[0])
	}
	snapshot, ok, err := managementstate.NewStore(state.statePath, nil).Load()
	if err != nil || !ok || !snapshot.Setup.Completed || len(snapshot.Users) != 1 {
		t.Fatalf("snapshot=%+v ok=%v err=%v", snapshot, ok, err)
	}
}

func TestSetupCompleteRejectsUnavailableExposure(t *testing.T) {
	state := newTestSetupState(t, false)
	rec := httptest.NewRecorder()

	state.handleSetupComplete(rec, newSetupCompleteRequest())

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetupCompleteIsSingleUse(t *testing.T) {
	state := newTestSetupState(t, true)
	first := httptest.NewRecorder()
	state.handleSetupComplete(first, newSetupCompleteRequest())
	second := httptest.NewRecorder()
	state.handleSetupComplete(second, newSetupCompleteRequest())

	if first.Code != http.StatusCreated || second.Code != http.StatusConflict {
		t.Fatalf("first=%d second=%d body=%s", first.Code, second.Code, second.Body.String())
	}
}

func TestSetupCompleteValidatesPasswordAndBackupAcknowledgement(t *testing.T) {
	state := newTestSetupState(t, true)
	cases := []string{
		`{"username":"admin","password":"short","backupAcknowledged":true}`,
		`{"username":"admin","password":"a-long-secure-password","backupAcknowledged":false}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		state.handleSetupComplete(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestRouterAllowsUnauthenticatedLocalSetupOnly(t *testing.T) {
	dir := t.TempDir()
	router, _ := NewRouter(ServerInfo{
		Version:      "test",
		Mode:         "server",
		StatePath:    filepath.Join(dir, "state.json"),
		KeyPath:      filepath.Join(dir, "state.key"),
		PanelAccess:  "local",
		PanelListen:  "127.0.0.1:2096",
		SetupAllowed: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response SetupStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.Required || !response.Allowed {
		t.Fatalf("response=%+v", response)
	}
}

func newTestSetupState(t *testing.T, allowed bool) *managementState {
	t.Helper()
	return &managementState{
		statePath:    filepath.Join(t.TempDir(), "state.json"),
		applyRoot:    t.TempDir(),
		setupAllowed: allowed,
		settings: Settings{
			PanelListen: "127.0.0.1:2096",
			PanelAccess: "local",
			Mode:        "server",
		},
	}
}

func newSetupCompleteRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(
		`{"username":"admin","password":"a-long-secure-password","backupAcknowledged":true}`,
	))
	req.Header.Set("Content-Type", "application/json")
	return req
}
