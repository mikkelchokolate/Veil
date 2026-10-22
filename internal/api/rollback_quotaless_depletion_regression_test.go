package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// #1350: the rollback graft OR-ed the live depleted flag onto every retained
// snapshot client, including one whose restored configuration has no quota.
// That committed depleted=true with quotaBytes=null — a state every live
// writer refuses (validate: "depletion state requires quotaBytes") — into
// the clients table, state.json, the new immutable revision and the
// in-memory snapshot, and left the client's pending "depleted" enforcement
// target active. Restoring "no quota" is an explicit transition to
// unlimited, exactly like a quota clear through the API: the derived
// depletion state is dropped and the enforcement target superseded.
func TestRollbackToQuotalessSnapshotDropsLiveDepletion(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1024,`, func(s *managementSnapshot) {
		s.Clients[0].QuotaBytes = nil
		s.Clients[0].QuotaResetPolicy = "never"
		s.Clients[0].QuotaResetAt = nil
		s.Clients[0].Depleted = false
	})
	clients, err := state.clientRepo.AllClients()
	if err != nil || len(clients) != 1 {
		t.Fatalf("fixture clients: %+v err=%v", clients, err)
	}
	clientID := clients[0].ID
	// The client exhausts its live quota after the snapshot was taken.
	if _, err := state.db.Exec(`UPDATE clients SET depleted=1 WHERE id=?`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,desired_revision,state,updated_at)
  VALUES(?,1,?,1,1,'pending',1)`, clientID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}

	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusOK {
		t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
	}

	restored, err := state.clientRepo.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.QuotaBytes != nil {
		t.Fatalf("restored quotaBytes = %d, want null", *restored.QuotaBytes)
	}
	if restored.Depleted {
		t.Fatal("rollback committed depleted=true on a client restored without a quota")
	}

	var active int
	if err := state.db.QueryRow(`SELECT COUNT(*) FROM quota_enforcement WHERE client_id=? AND state<>'superseded'`, clientID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("%d quota enforcement target(s) still active for a client restored to unlimited", active)
	}

	state.mu.Lock()
	inMemory, err := state.snapshotLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	persisted, _, err := managementstate.NewStore(state.statePath, state.cipher).Load()
	if err != nil {
		t.Fatal(err)
	}
	for label, clients := range map[string][]model.ClientSnapshot{"in-memory snapshot": inMemory.Clients, "state.json": persisted.Clients} {
		for _, item := range clients {
			if item.ID == clientID && (item.Depleted || item.QuotaBytes != nil) {
				t.Fatalf("%s carries quota=%v depleted=%v, want null/false", label, item.QuotaBytes, item.Depleted)
			}
		}
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
	for _, item := range stored.Clients {
		if item.ID == clientID && (item.Depleted || item.QuotaBytes != nil) {
			t.Fatalf("new immutable revision carries quota=%v depleted=%v, want null/false", item.QuotaBytes, item.Depleted)
		}
	}
}

// Control: a snapshot that still carries a quota keeps the live depletion
// (#1099 monotonic merge) — the fix above must not un-deplete every client.
func TestRollbackWithQuotaKeepsLiveDepletion(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1024,`, func(s *managementSnapshot) {
		s.Clients[0].Depleted = false
	})
	clients, err := state.clientRepo.AllClients()
	if err != nil || len(clients) != 1 {
		t.Fatalf("fixture clients: %+v err=%v", clients, err)
	}
	if _, err := state.db.Exec(`UPDATE clients SET depleted=1 WHERE id=?`, clients[0].ID); err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusOK {
		t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
	}
	restored, err := state.clientRepo.Get(clients[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Depleted {
		t.Fatal("rollback un-depleted a client whose restored configuration still has a quota")
	}
}
