package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// rollbackCaseFixture creates one hysteria2 client binding, stores a mutated
// copy of its revision as an older immutable revision, and returns the
// selected revision number together with the binding ID (#1273 item 1).
func rollbackCaseFixture(t *testing.T, mutate func(*managementSnapshot)) (http.Handler, *managementState, uint64, string) {
	t.Helper()
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"rollback-case-hy","protocol":"hysteria2","transport":"udp","port":29443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"rollback-case-client","bindings":[{"inboundId":"rollback-case-hy","runtimeIdentity":"case_identity","credential":"credential"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", created.Code, created.Body.String())
	}
	bindings, err := state.clientRepo.AllBindings()
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings: count=%d err=%v", len(bindings), err)
	}
	current, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(current.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := state.decryptSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}
	mutate(&snapshot)
	if err := state.encryptSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}
	mutated, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := state.applyRevisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.applySnapshots.Save(selected, mutated); err != nil {
		t.Fatal(err)
	}
	if _, err := state.applyRevisions.BumpDesired(); err != nil {
		t.Fatal(err)
	}
	return router, state, selected, bindings[0].ID
}

func TestRollbackRejectsCaseFoldedDuplicateRuntimeIdentity(t *testing.T) {
	router, _, selected, _ := rollbackCaseFixture(t, func(snapshot *managementSnapshot) {
		duplicate := snapshot.Bindings[0]
		duplicate.ID = "case-duplicate-binding-id"
		duplicate.RuntimeIdentity = "CASE_IDENTITY"
		snapshot.Bindings = append(snapshot.Bindings, duplicate)
	})
	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("case-folded duplicate identity status = %d, want 422: %s", response.Code, response.Body.String())
	}
}

func TestRollbackAcceptsLegacyMixedCaseSnapshotIdentity(t *testing.T) {
	// Pre-#1111 revisions may carry mixed-case identities; migration 29
	// folded the live rows, and the graft carries the live identity forward,
	// so such a revision must stay restorable.
	router, state, selected, bindingID := rollbackCaseFixture(t, func(snapshot *managementSnapshot) {
		snapshot.Bindings[0].RuntimeIdentity = "Case_Identity"
	})
	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusOK {
		t.Fatalf("legacy mixed-case rollback status = %d, want 200: %s", response.Code, response.Body.String())
	}
	assertRollbackIdentity(t, state, bindingID, "case_identity")
}

func TestRollbackNeverRestoresMixedCaseRuntimeIdentity(t *testing.T) {
	router, state, selected, bindingID := rollbackCaseFixture(t, func(*managementSnapshot) {})
	// A mixed-case live row (only reachable through out-of-band DB edits
	// since migration 29) must not be carried forward verbatim: hysteria2
	// reports lowercased usernames, so the binding's traffic would be lost.
	if _, err := state.db.Exec(`UPDATE client_bindings SET runtime_identity=?, version=version+1 WHERE id=?`, "Mixed_Live_Identity", bindingID); err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusOK {
		t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
	}
	assertRollbackIdentity(t, state, bindingID, "mixed_live_identity")
}

func assertRollbackIdentity(t *testing.T, state *managementState, bindingID, want string) {
	t.Helper()
	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range bindings {
		if binding.ID == bindingID {
			found = true
			if binding.RuntimeIdentity != want {
				t.Errorf("stored runtime identity = %q, want %q", binding.RuntimeIdentity, want)
			}
		}
	}
	if !found {
		t.Fatalf("binding %q missing after rollback", bindingID)
	}
	current, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(current.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var stored managementSnapshot
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	for _, binding := range stored.Bindings {
		if binding.ID == bindingID && binding.RuntimeIdentity != want {
			t.Errorf("stored revision runtime identity = %q, want %q", binding.RuntimeIdentity, want)
		}
	}
}

// The post-graft check is unreachable through HTTP (every retained binding
// carries its lowercased live identity), so pin it directly (#1273).
func TestValidateRollbackRuntimeIdentities(t *testing.T) {
	binding := func(id, inbound, identity string) model.BindingSnapshot {
		return model.BindingSnapshot{ID: id, InboundID: inbound, RuntimeIdentity: identity}
	}
	tests := []struct {
		name     string
		bindings []model.BindingSnapshot
		wantErr  bool
	}{
		{name: "valid", bindings: []model.BindingSnapshot{binding("a", "in", "ok_id"), binding("b", "in", "other-id"), binding("c", "in2", "ok_id")}},
		{name: "mixed_case", bindings: []model.BindingSnapshot{binding("a", "in", "Mixed_ID")}, wantErr: true},
		{name: "empty", bindings: []model.BindingSnapshot{binding("a", "in", "")}, wantErr: true},
		{name: "too_long", bindings: []model.BindingSnapshot{binding("a", "in", strings.Repeat("a", 49))}, wantErr: true},
		{name: "invalid_char", bindings: []model.BindingSnapshot{binding("a", "in", "bad.id")}, wantErr: true},
		{name: "duplicate_per_inbound", bindings: []model.BindingSnapshot{binding("a", "in", "same"), binding("b", "in", "same")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRollbackRuntimeIdentities(managementSnapshot{Bindings: test.bindings})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateRollbackRuntimeIdentities err = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
