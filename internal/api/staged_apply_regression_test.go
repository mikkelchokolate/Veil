package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestManualStagedApplyKeepsSubscriptionOnPreviousAppliedRevision(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"staged-subscription-hy","protocol":"hysteria2","transport":"udp","port":29443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"staged-subscription-client","bindings":[{"inboundId":"staged-subscription-hy","runtimeIdentity":"staged_subscription_identity","credential":"applied-staged-secret"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", created.Code, created.Body.String())
	}
	client := unwrapClient(t, created.Body.Bytes())
	clientID := client["id"].(string)
	bindingID := client["bindings"].([]any)[0].(map[string]any)["id"].(string)
	issued, err := state.tokenStore.Issue(clientID, "staged-subscription", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := publicRawSubscription(t, router, issued.Plaintext)
	if before.Code != http.StatusOK {
		t.Fatalf("baseline subscription: %d %s", before.Code, before.Body.String())
	}
	beforeRevisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.clientCreds.Rotate(bindingID, "password", "desired-staged-secret"); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	_, err = state.bumpDesiredRevisionLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	staged := v1Request(t, router, http.MethodPost, "/api/apply",
		`{"confirm":true,"applyLive":false,"applyServices":false}`)
	if staged.Code != http.StatusOK {
		t.Fatalf("staged apply: %d %s", staged.Code, staged.Body.String())
	}
	after := publicRawSubscription(t, router, issued.Plaintext)
	if after.Code != http.StatusOK {
		t.Fatalf("subscription after staged apply: %d %s", after.Code, after.Body.String())
	}
	if after.Body.String() != before.Body.String() {
		t.Fatalf("staged-only apply changed subscription\nbefore=%q\nafter=%q", before.Body.String(), after.Body.String())
	}
	if got := after.Header().Get("X-Veil-Applied-Revision"); got != fmt.Sprint(beforeRevisions.Applied) {
		t.Fatalf("applied revision header=%q, want %d", got, beforeRevisions.Applied)
	}
}

func TestManualStagedApplyDoesNotPublishOrAdvanceAppliedRevision(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	desired, err := state.bumpDesiredRevisionLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if desired <= before.Applied {
		t.Fatalf("test setup did not create a pending revision: before=%+v desired=%d", before, desired)
	}

	response := v1Request(t, router, http.MethodPost, "/api/apply",
		`{"confirm":true,"applyLive":false,"applyServices":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("staged apply status=%d: %s", response.Code, response.Body.String())
	}
	var body ApplyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Applied {
		t.Fatalf("staged-only response claims applied=true: %+v", body)
	}

	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if after.Applied != before.Applied || after.Desired != desired {
		t.Fatalf("staged-only apply changed applied revision: before=%+v after=%+v", before, after)
	}
	jobs, err := state.applyJobs.List(1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("latest staged job: jobs=%+v err=%v", jobs, err)
	}
	if jobs[0].Status != "staged" {
		t.Fatalf("staged-only job status=%q, want staged", jobs[0].Status)
	}
	var receipts int
	if err := state.db.QueryRow(`SELECT COUNT(*) FROM runtime_publications WHERE job_id=?`, jobs[0].ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("staged-only apply created %d runtime publication receipts", receipts)
	}
}
