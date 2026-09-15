package api

import (
	"encoding/json"
	"net/http"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
)

func TestRoutingPresetApplyUsesDurableFencedRunner(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	createFenceTestInbound(t, router)
	useDefaultFenceRequiredHelper(t, state)
	before, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	beforeRev, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}

	response := v1Request(t, router, http.MethodPost, "/api/routing/presets/all", "")
	if response.Code != http.StatusOK {
		t.Fatalf("preset apply: %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preset body: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("preset apply claimed failure: %s", response.Body.String())
	}
	if _, ok := body["applyJob"]; !ok {
		t.Fatalf("preset apply returned a bare preset body without applyJob: %s", response.Body.String())
	}

	after, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("preset apply jobs=%d -> %d, want exactly one durable Runner job", len(before), len(after))
	}
	job := after[0]
	if job.Status != veilapply.StatusSucceeded || job.LeaseGeneration == 0 || job.OwnerProcess == "" {
		t.Fatalf("preset apply was not durably fenced and finalized: %+v", job)
	}
	afterRev, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if afterRev.Desired <= beforeRev.Desired || afterRev.Applied != afterRev.Desired {
		t.Fatalf("preset apply revisions before=%+v after=%+v", beforeRev, afterRev)
	}
}
