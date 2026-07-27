package api

import (
	"net/http"
	"strconv"
	"testing"
)

func TestIntentionalRollbackCreatesNewDesiredRevisionWithoutDecrementing(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	createdResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{
		"name":"selected-name",
		"email":"selected@example.test",
		"enabled":true,
		"groupId":"selected-group",
		"quotaBytes":123456,
		"quotaResetPolicy":"weekly",
		"quotaResetAt":1893456000,
		"expiresAt":1924992000,
		"deviceLimit":4,
		"notes":"selected-notes"
	}`)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	created := unwrapClient(t, createdResponse.Body.Bytes())
	clientID := created["id"].(string)
	first, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if first.Desired == 0 || first.Applied != first.Desired {
		t.Fatalf("first revision not applied: %+v", first)
	}
	selectedPayload, err := state.applySnapshots.Load(first.Desired)
	if err != nil {
		t.Fatal(err)
	}

	updatedResponse := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
		`{"version":1,"name":"newer-name","notes":"newer-notes","groupId":"newer-group"}`)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if before.Desired != first.Desired+1 || before.Applied != before.Desired {
		t.Fatalf("update revisions: %+v first=%+v", before, first)
	}
	jobsBefore, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}

	unconfirmed := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		`{"selectedRevision":`+strconv.FormatUint(first.Desired, 10)+`,"confirm":false}`)
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed rollback: %d %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	unchanged, _ := state.applyRevisions.Get()
	if unchanged != before {
		t.Fatalf("unconfirmed rollback touched revisions: before=%+v after=%+v", before, unchanged)
	}

	confirmed := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		`{"selectedRevision":`+strconv.FormatUint(first.Desired, 10)+`,"confirm":true}`)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirmed rollback: %d %s", confirmed.Code, confirmed.Body.String())
	}
	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired != before.Desired+1 || after.Applied != after.Desired {
		t.Fatalf("rollback decremented or failed to apply revisions: before=%+v after=%+v", before, after)
	}
	newPayload, err := state.applySnapshots.Load(after.Desired)
	if err != nil {
		t.Fatal(err)
	}
	if string(newPayload) != string(selectedPayload) {
		t.Fatal("new desired revision does not contain the selected immutable snapshot")
	}
	restored, err := state.clientService.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != "selected-name" || restored.Notes != "selected-notes" || restored.GroupID == nil || *restored.GroupID != "selected-group" {
		t.Fatalf("durable client record was not restored losslessly: %+v", restored.Client)
	}
	jobsAfter, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobsAfter) != len(jobsBefore)+1 {
		t.Fatalf("rollback jobs=%d before=%d, want exactly one new apply", len(jobsAfter), len(jobsBefore))
	}
	var selectedRevision, newRevision uint64
	var rows int
	if err := state.db.QueryRow(`SELECT COUNT(*), MIN(selected_revision), MIN(new_revision) FROM apply_rollbacks`).Scan(&rows, &selectedRevision, &newRevision); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || selectedRevision != first.Desired || newRevision != after.Desired {
		t.Fatalf("immutable rollback audit mismatch: rows=%d selected=%d new=%d", rows, selectedRevision, newRevision)
	}
}
