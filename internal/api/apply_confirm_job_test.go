package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
)

func TestManualApplyWithoutConfirmDoesNotCreateJob(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	before, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{}`, `{"confirm":false}`} {
		response := v1Request(t, router, http.MethodPost, "/api/apply", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want=400: %s", body, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "confirm=true is required") {
			t.Fatalf("body=%s error=%s", body, response.Body.String())
		}
	}

	after, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("unconfirmed apply recorded jobs %d -> %d", len(before), len(after))
	}

	stateResponse := v1Request(t, router, http.MethodGet, "/api/apply/state", "")
	if stateResponse.Code != http.StatusOK {
		t.Fatalf("apply state status=%d: %s", stateResponse.Code, stateResponse.Body.String())
	}
	var view applyStateResponse
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State == veilapply.StateFailed {
		t.Fatalf("unconfirmed apply left panel state=%q lastError=%+v", view.State, view.LastError)
	}
}

func TestDeriveSystemStateFailedJobWhenAlreadyApplied(t *testing.T) {
	failed := &veilapply.Job{Status: veilapply.StatusFailed, ErrorCode: "APPLY_ERROR", ErrorMessage: "confirm=true is required to write staged apply files"}
	if got := deriveSystemState(veilapply.Revisions{Desired: 82, Applied: 82}, failed); got != veilapply.StateSynced {
		t.Fatalf("synced revisions with failed no-op job: state=%q want=%q", got, veilapply.StateSynced)
	}
	if got := deriveSystemState(veilapply.Revisions{Desired: 83, Applied: 82}, failed); got != veilapply.StateFailed {
		t.Fatalf("pending revisions with failed job: state=%q want=%q", got, veilapply.StateFailed)
	}
}
