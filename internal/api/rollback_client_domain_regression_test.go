package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

// rollbackDomainFixture creates one hysteria2 inbound and one client bound
// to it (the client JSON is merged into the create request), then plants a
// mutated copy of the resulting revision as an older immutable revision the
// rollback route may select.
func rollbackDomainFixture(t *testing.T, clientFields string, mutate func(*managementSnapshot)) (http.Handler, *managementState, uint64) {
	t.Helper()
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"rollback-domain-hy","protocol":"hysteria2","transport":"udp","port":28543,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"rollback-domain-client",`+clientFields+`"bindings":[{"inboundId":"rollback-domain-hy","runtimeIdentity":"rollback_domain_identity","credential":"credential"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", created.Code, created.Body.String())
	}
	current, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(current.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var planted managementSnapshot
	if err := json.Unmarshal(payload, &planted); err != nil {
		t.Fatal(err)
	}
	if err := state.decryptSnapshot(&planted); err != nil {
		t.Fatal(err)
	}
	if len(planted.Clients) != 1 || len(planted.Bindings) != 1 || len(planted.Inbounds) != 1 {
		t.Fatalf("unexpected fixture snapshot: %d clients, %d bindings, %d inbounds",
			len(planted.Clients), len(planted.Bindings), len(planted.Inbounds))
	}
	mutate(&planted)
	if err := state.encryptSnapshot(&planted); err != nil {
		t.Fatal(err)
	}
	plantedPayload, err := json.Marshal(planted)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := state.applyRevisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.applySnapshots.Save(selected, plantedPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := state.applyRevisions.BumpDesired(); err != nil {
		t.Fatal(err)
	}
	return router, state, selected
}

// assertRollbackRefusedUnchanged runs the rollback and requires an honest 422
// that leaves the state file, revisions, and client tables untouched.
func assertRollbackRefusedUnchanged(t *testing.T, router http.Handler, state *managementState, selected uint64, wantInBody string) {
	t.Helper()
	stateBefore, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	revisionsBefore, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	clientsBefore, err := state.clientRepo.AllClients()
	if err != nil {
		t.Fatal(err)
	}
	bindingsBefore, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}

	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rollback status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), wantInBody) {
		t.Fatalf("rollback refusal %q does not name %q", response.Body.String(), wantInBody)
	}

	stateAfter, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Error("state.json changed after a refused rollback")
	}
	if revisionsAfter, err := state.applyRevisions.Get(); err != nil || revisionsAfter != revisionsBefore {
		t.Errorf("revisions changed after a refused rollback: before=%+v after=%+v err=%v", revisionsBefore, revisionsAfter, err)
	}
	clientsAfter, _ := state.clientRepo.AllClients()
	bindingsAfter, _ := state.clientRepo.AllBindings()
	if !reflect.DeepEqual(clientsBefore, clientsAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) {
		t.Error("normalized clients changed after a refused rollback")
	}
}

// #1270: a rollback must run restored client rows through the same domain
// rules as every live writer. Before the fix the storage triggers turned some
// of these into an opaque 500 ("rollback commit failed") and let the rest
// (non-positive expiry, over-range quota, malformed name, depletion without
// a quota) commit outright.
func TestRollbackRefusesClientDomainValuesTheAPIRejects(t *testing.T) {
	intPtr := func(v int) *int { return &v }
	int64Ptr := func(v int64) *int64 { return &v }
	cases := []struct {
		name   string
		mutate func(*managementSnapshot)
		want   string
	}{
		{"device_limit_zero", func(s *managementSnapshot) { s.Clients[0].DeviceLimit = intPtr(0) }, "deviceLimit"},
		{"ip_limit_negative", func(s *managementSnapshot) { s.Clients[0].IPLimit = intPtr(-1) }, "ipLimit"},
		{"quota_negative", func(s *managementSnapshot) { s.Clients[0].QuotaBytes = int64Ptr(-1) }, "quotaBytes"},
		{"quota_over_safe_integer", func(s *managementSnapshot) { s.Clients[0].QuotaBytes = int64Ptr(1 << 62) }, "quotaBytes"},
		{"unknown_reset_policy", func(s *managementSnapshot) {
			s.Clients[0].QuotaBytes = int64Ptr(1024)
			s.Clients[0].QuotaResetPolicy = "hourly"
		}, "quotaResetPolicy"},
		{"depleted_without_quota", func(s *managementSnapshot) { s.Clients[0].Depleted = true }, "requires quotaBytes"},
		{"expiry_zero", func(s *managementSnapshot) { s.Clients[0].ExpiresAt = int64Ptr(0) }, "expiresAt"},
		{"name_surrounding_whitespace", func(s *managementSnapshot) { s.Clients[0].Name = " padded " }, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, state, selected := rollbackDomainFixture(t, "", tc.mutate)
			assertRollbackRefusedUnchanged(t, router, state, selected, tc.want)
		})
	}
}

// #1270 / #1224: limits restored onto a binding whose (restored) inbound does
// not enforce them would commit silently inert. The rollback must refuse it
// just as validateConnectionLimitBindingsLocked refuses the live write.
func TestRollbackRefusesConnectionLimitsOnIncapableInbound(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*managementSnapshot)
	}{
		{"inbound_disabled", func(s *managementSnapshot) { s.Inbounds[0].Enabled = false }},
		{"inbound_protocol_without_device_limits", func(s *managementSnapshot) {
			// Inbound stays enabled; only the protocolConnectionLimits half
			// of the capability predicate fails.
			s.Inbounds[0].Protocol = "mieru"
		}},
		{"ip_limit_inbound_disabled", func(s *managementSnapshot) {
			s.Clients[0].DeviceLimit = nil
			v := 3
			s.Clients[0].IPLimit = &v
			s.Inbounds[0].Enabled = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, state, selected := rollbackDomainFixture(t, `"deviceLimit":2,`, tc.mutate)
			assertRollbackRefusedUnchanged(t, router, state, selected, "device/ip limits are unsupported for inbound rollback-domain-hy")
		})
	}
}

// The capability verdict covers the rows the rollback actually writes: a
// binding disabled live after the snapshot stays disabled (#1099), so its
// limits are not inert and the rollback must go through. The control case
// (limits on a capable inbound) must go through as well, so the check above
// is not just refusing every limited client.
func TestRollbackConnectionLimitCheckFollowsMergedBindings(t *testing.T) {
	t.Run("capable_inbound", func(t *testing.T) {
		router, state, selected := rollbackDomainFixture(t, `"deviceLimit":2,`, func(*managementSnapshot) {})
		before, err := state.applyRevisions.Get()
		if err != nil {
			t.Fatal(err)
		}
		response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
			jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
		if response.Code != http.StatusOK {
			t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
		}
		if after, _ := state.applyRevisions.Get(); after.Desired != before.Desired+1 {
			t.Fatalf("desired revision = %d, want %d", after.Desired, before.Desired+1)
		}
	})
	t.Run("binding_disabled_live", func(t *testing.T) {
		router, state, selected := rollbackDomainFixture(t, `"deviceLimit":2,`, func(s *managementSnapshot) {
			s.Inbounds[0].Enabled = false
		})
		if _, err := state.db.Exec(`UPDATE client_bindings SET enabled=0`); err != nil {
			t.Fatal(err)
		}
		before, err := state.applyRevisions.Get()
		if err != nil {
			t.Fatal(err)
		}
		response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
			jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
		if response.Code != http.StatusOK {
			t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
		}
		if after, _ := state.applyRevisions.Get(); after.Desired != before.Desired+1 {
			t.Fatalf("desired revision = %d, want %d", after.Desired, before.Desired+1)
		}
		bindings, err := state.clientRepo.AllBindings()
		if err != nil {
			t.Fatal(err)
		}
		if len(bindings) != 1 || bindings[0].Enabled {
			t.Fatalf("live-disabled binding was re-enabled by rollback: %+v", bindings)
		}
	})
}

// A client deleted live since the snapshot is dropped by the graft (#1099)
// and never written, so a legacy-invalid value on it must not veto the
// rollback: the CHANGELOG remedy ("fix the client in the current revision")
// cannot apply to a client that no longer exists (#1270 review).
func TestRollbackIgnoresDomainValuesOfClientsDeletedLive(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, "", func(s *managementSnapshot) {
		ghost := s.Clients[0]
		ghost.ID = "rollback-domain-ghost"
		ghost.Name = "rollback-domain-ghost"
		zero := 0
		ghost.DeviceLimit = &zero
		s.Clients = append(s.Clients, ghost)
	})
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
		jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
	if response.Code != http.StatusOK {
		t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if after, _ := state.applyRevisions.Get(); after.Desired != before.Desired+1 {
		t.Fatalf("desired revision = %d, want %d", after.Desired, before.Desired+1)
	}
	clients, err := state.clientRepo.AllClients()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range clients {
		if item.ID == "rollback-domain-ghost" {
			t.Fatalf("client deleted live was resurrected by rollback: %+v", item)
		}
	}
}

// The same invalid value on a client the rollback DOES restore is still
// refused, so the retained-client filter above is not skipping everything.
func TestRollbackStillRefusesDomainValuesOfRetainedClients(t *testing.T) {
	router, state, selected := rollbackDomainFixture(t, "", func(s *managementSnapshot) {
		ghost := s.Clients[0]
		ghost.ID = "rollback-domain-ghost"
		ghost.Name = "rollback-domain-ghost"
		s.Clients = append(s.Clients, ghost)
		zero := 0
		s.Clients[0].DeviceLimit = &zero
	})
	assertRollbackRefusedUnchanged(t, router, state, selected, "deviceLimit")
}

// #1351 / #1173: a quota restored onto a binding whose (restored) inbound
// cannot enforce it would commit silently inert: the runtime never rejects
// the depleted client while the API shows the quota as set, and the next
// quota edit on that client is refused with 400. The rollback must refuse it
// just as validateQuotaBindingsLocked refuses the live write.
func TestRollbackRefusesQuotaOnIncapableInbound(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*managementSnapshot)
	}{
		{"inbound_disabled", func(s *managementSnapshot) { s.Inbounds[0].Enabled = false }},
		{"inbound_protocol_without_quota_enforcement", func(s *managementSnapshot) {
			// Inbound stays enabled; only the protocolQuotaEnforcement half
			// of the capability predicate fails.
			s.Inbounds[0].Protocol = "mieru"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1048576,`, tc.mutate)
			assertRollbackRefusedUnchanged(t, router, state, selected, "quota enforcement is unsupported for inbound rollback-domain-hy")
		})
	}
}

// The quota capability verdict covers exactly the rows the rollback writes:
// a quota on a capable inbound goes through, a binding disabled live after
// the snapshot stays disabled (#1099) and so does not count, and a client
// without a quota is never checked. Without these controls the refusal above
// could pass by refusing every rollback that touches the inbound.
func TestRollbackQuotaCheckFollowsMergedBindings(t *testing.T) {
	rollback := func(t *testing.T, router http.Handler, state *managementState, selected uint64) {
		t.Helper()
		before, err := state.applyRevisions.Get()
		if err != nil {
			t.Fatal(err)
		}
		response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
			jsonBody(t, map[string]any{"selectedRevision": selected, "confirm": true}))
		if response.Code != http.StatusOK {
			t.Fatalf("rollback status = %d, want 200: %s", response.Code, response.Body.String())
		}
		if after, _ := state.applyRevisions.Get(); after.Desired != before.Desired+1 {
			t.Fatalf("desired revision = %d, want %d", after.Desired, before.Desired+1)
		}
	}
	t.Run("capable_inbound", func(t *testing.T) {
		router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1048576,`, func(*managementSnapshot) {})
		rollback(t, router, state, selected)
		clients, err := state.clientRepo.AllClients()
		if err != nil {
			t.Fatal(err)
		}
		if len(clients) != 1 || clients[0].QuotaBytes == nil || *clients[0].QuotaBytes != 1048576 {
			t.Fatalf("quota not restored on a capable inbound: %+v", clients)
		}
	})
	t.Run("binding_disabled_live", func(t *testing.T) {
		router, state, selected := rollbackDomainFixture(t, `"quotaBytes":1048576,`, func(s *managementSnapshot) {
			s.Inbounds[0].Enabled = false
		})
		if _, err := state.db.Exec(`UPDATE client_bindings SET enabled=0`); err != nil {
			t.Fatal(err)
		}
		rollback(t, router, state, selected)
		bindings, err := state.clientRepo.AllBindings()
		if err != nil {
			t.Fatal(err)
		}
		if len(bindings) != 1 || bindings[0].Enabled {
			t.Fatalf("live-disabled binding was re-enabled by rollback: %+v", bindings)
		}
	})
	t.Run("quotaless_client_on_disabled_inbound", func(t *testing.T) {
		router, state, selected := rollbackDomainFixture(t, "", func(s *managementSnapshot) {
			s.Inbounds[0].Enabled = false
		})
		rollback(t, router, state, selected)
	})
}
