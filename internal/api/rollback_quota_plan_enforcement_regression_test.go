package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// #1355: POST /api/apply/rollback restoring a client that still has a quota,
// but with different bytes or reset policy than the live row, left the live
// quota-enforcement record bound to the old plan active, so enforcement kept
// using the old values. The rollback must supersede it in its own tx, like a
// live plan change (prepareQuotaResetTransition).
func TestRollbackWithChangedQuotaPlanSupersedesEnforcementRecord(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1024,`, func(s *managementSnapshot) {
		raised := int64(4096)
		s.Clients[0].QuotaBytes = &raised
		s.Clients[0].QuotaResetPolicy = "daily"
		s.Clients[0].QuotaResetAt = nil
	})
	clients, err := state.clientRepo.AllClients()
	if err != nil || len(clients) != 1 {
		t.Fatalf("fixture clients: %+v err=%v", clients, err)
	}
	clientID := clients[0].ID
	if clients[0].QuotaBytes == nil || *clients[0].QuotaBytes != 1024 || clients[0].QuotaResetPolicy != "never" {
		t.Fatalf("fixture live plan quota=%v policy=%q, want 1024/never", clients[0].QuotaBytes, clients[0].QuotaResetPolicy)
	}
	// The live 1024-byte plan is exhausted; its depleted target is pending.
	if _, err := state.db.Exec(`UPDATE clients SET depleted=1 WHERE id=?`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,desired_revision,state,updated_at)
  VALUES(?,?,?,1,0,'pending',1)`, clientID, clients[0].Version+1, strings.Repeat("a", 64)); err != nil {
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
	if restored.QuotaBytes == nil || *restored.QuotaBytes != 4096 || restored.QuotaResetPolicy != "daily" {
		t.Fatalf("restored quota=%v policy=%q, want 4096/daily", restored.QuotaBytes, restored.QuotaResetPolicy)
	}
	var state0 string
	if err := state.db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=? AND target_payload_hash=?`,
		clientID, strings.Repeat("a", 64)).Scan(&state0); err != nil {
		t.Fatal(err)
	}
	if state0 != "superseded" {
		t.Fatalf("enforcement record bound to the old 1024-byte/never plan is %q after rollback, want superseded", state0)
	}

	// The new immutable revision carries the restored plan.
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
	found := false
	for _, item := range stored.Clients {
		if item.ID != clientID {
			continue
		}
		found = true
		if item.QuotaBytes == nil || *item.QuotaBytes != 4096 || item.QuotaResetPolicy != "daily" {
			t.Errorf("new immutable revision carries quota=%v policy=%q, want 4096/daily", item.QuotaBytes, item.QuotaResetPolicy)
		}
	}
	if !found {
		t.Errorf("new immutable revision does not contain the restored client %s", clientID)
	}
}

// Control: restoring the same quota plan keeps the live enforcement record.
func TestRollbackWithSameQuotaPlanKeepsEnforcementRecord(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1024,`, func(s *managementSnapshot) {
		s.Clients[0].Notes = "restored"
	})
	clients, err := state.clientRepo.AllClients()
	if err != nil || len(clients) != 1 {
		t.Fatalf("fixture clients: %+v err=%v", clients, err)
	}
	clientID := clients[0].ID
	if _, err := state.db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,desired_revision,state,updated_at)
  VALUES(?,?,?,1,0,'pending',1)`, clientID, clients[0].Version+1, strings.Repeat("b", 64)); err != nil {
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
	if restored.Notes != "restored" {
		t.Fatalf("restored notes=%q, want %q (rollback did not restore the snapshot)", restored.Notes, "restored")
	}
	var recordState string
	if err := state.db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=? AND target_payload_hash=?`,
		clientID, strings.Repeat("b", 64)).Scan(&recordState); err != nil {
		t.Fatal(err)
	}
	if recordState != "pending" {
		t.Fatalf("enforcement record for an unchanged quota plan is %q after rollback, want pending", recordState)
	}
}
