package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

// failingSaveState returns a bare managementState whose every saveLocked
// fails: the state path sits under a regular file, so the snapshot barrier's
// MkdirAll cannot create it. Rules/source/audit are seeded for assertions.
func failingSaveState(t *testing.T) (*managementState, *audit.Recorder) {
	t.Helper()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	recorder := audit.NewRecorder(filepath.Join(dir, "audit.jsonl"), audit.RecorderOptions{})
	s := &managementState{
		statePath: filepath.Join(blocker, "state.json"),
		audit:     recorder,
	}
	return s, recorder
}

func auditActionsFor(t *testing.T, recorder *audit.Recorder, action string) []audit.Record {
	t.Helper()
	records, err := recorder.List(100, time.Time{})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var out []audit.Record
	for _, record := range records {
		if record.Action == action {
			out = append(out, record)
		}
	}
	return out
}

// TestRoutingRuleCreateFailureLeavesNoCommittedRuleOrSource covers
// #998/#1054: the dat source is staged with the candidate rule set and the
// rule+source persist in one mutation save. When that save fails, the
// handler must return an error AND leave no residue — no committed rule, no
// staged source leaked into memory — and the audit entry must record the
// failure, never a success recorded before the fallible save.
func TestRoutingRuleCreateFailureLeavesNoCommittedRuleOrSource(t *testing.T) {
	s, recorder := failingSaveState(t)
	s.rules = []RoutingRule{{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	// A partial-default source already carrying geoip.dat: the staged merge
	// would have added geosite.dat for the new rule.
	partialSource := RoutingSource{
		Repository: "custom-repo",
		Files:      []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat"}},
	}
	s.routingSource = partialSource

	req := httptest.NewRequest(http.MethodPost, "/api/routing/rules",
		strings.NewReader(`{"name":"ru-sites","match":"geosite:category-ru","outbound":"proxy","enabled":true}`))
	rec := httptest.NewRecorder()
	s.handleRoutingRules(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/routing/rules status = %d, want 500", rec.Code)
	}
	if len(s.rules) != 1 || s.rules[0].Name != "default-direct" {
		t.Fatalf("failed mutation left committed rules: %+v", s.rules)
	}
	if len(s.routingSource.Files) != 1 || s.routingSource.Repository != partialSource.Repository {
		t.Fatalf("staged source leaked after failed save: %+v", s.routingSource)
	}
	records := auditActionsFor(t, recorder, "create_routing_rule")
	if len(records) != 1 || records[0].Success {
		t.Fatalf("create_routing_rule audit records = %+v, want exactly one failure", records)
	}
}

// TestRoutingRuleUpdateFailureLeavesNoCommittedRuleOrSource is the PUT twin:
// the staged source merge is rolled back together with the rejected update.
func TestRoutingRuleUpdateFailureLeavesNoCommittedRuleOrSource(t *testing.T) {
	s, recorder := failingSaveState(t)
	s.rules = []RoutingRule{{Name: "sites", Match: "geosite:category-ru", Outbound: "proxy", Enabled: true}}
	partialSource := RoutingSource{
		Files: []RoutingSourceFile{{Name: "geosite.dat", URL: "https://example.test/geosite.dat"}},
	}
	s.routingSource = partialSource

	req := httptest.NewRequest(http.MethodPut, "/api/routing/rules/sites",
		strings.NewReader(`{"match":"geoip:ru","outbound":"direct","enabled":true}`))
	rec := httptest.NewRecorder()
	s.handleRoutingRuleByName(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT /api/routing/rules/sites status = %d, want 500", rec.Code)
	}
	if len(s.rules) != 1 || s.rules[0].Match != "geosite:category-ru" || s.rules[0].Outbound != "proxy" {
		t.Fatalf("failed update left mutated rules: %+v", s.rules)
	}
	if len(s.routingSource.Files) != 1 || s.routingSource.Files[0].Name != "geosite.dat" {
		t.Fatalf("staged source leaked after failed save: %+v", s.routingSource)
	}
	records := auditActionsFor(t, recorder, "update_routing_rule")
	if len(records) != 1 || records[0].Success {
		t.Fatalf("update_routing_rule audit records = %+v, want exactly one failure", records)
	}
}

// TestRoutingRuleCreateCompletesPartialDatSourceInOneCommit covers #995 on
// the handler path: a source carrying only geoip.dat (attached when the
// earlier rule set needed just geoip) must gain geosite.dat when a geosite
// rule is added — and the merged source persists with the rule in the same
// committed state file.
func TestRoutingRuleCreateCompletesPartialDatSourceInOneCommit(t *testing.T) {
	prevAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = prevAutoApply })

	dir := t.TempDir()
	s := &managementState{statePath: filepath.Join(dir, "state.json")}
	s.rules = []RoutingRule{{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	s.routingSource = RoutingSource{
		Repository: "custom-repo",
		Files:      []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat"}},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/routing/rules",
		strings.NewReader(`{"name":"ru-sites","match":"geosite:category-ru","outbound":"proxy","enabled":true}`))
	rec := httptest.NewRecorder()
	s.handleRoutingRules(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/routing/rules status = %d body %s", rec.Code, rec.Body.String())
	}
	names := map[string]bool{}
	for _, file := range s.routingSource.Files {
		names[file.Name] = true
	}
	if !names["geoip.dat"] || !names["geosite.dat"] {
		t.Fatalf("partial source not completed: %+v", s.routingSource)
	}
	if s.routingSource.Repository != "custom-repo" {
		t.Fatalf("operator repository provenance lost: %+v", s.routingSource)
	}
	// The committed state file holds the rule and the completed source
	// together — the source cannot lag the rule by a second save.
	snapshot, found, err := managementstate.NewStore(s.statePath, nil).Load()
	if err != nil || !found {
		t.Fatalf("load state file: found=%v err=%v", found, err)
	}
	persistedNames := map[string]bool{}
	for _, file := range snapshot.RoutingSource.Files {
		persistedNames[file.Name] = true
	}
	if !persistedNames["geoip.dat"] || !persistedNames["geosite.dat"] {
		t.Fatalf("persisted source incomplete: %+v", snapshot.RoutingSource)
	}
	persistedRule := false
	for _, rule := range snapshot.Rules {
		if rule.Name == "ru-sites" {
			persistedRule = true
		}
	}
	if !persistedRule {
		t.Fatalf("persisted rules missing created rule: %+v", snapshot.Rules)
	}
}

// TestMigrateLegacyInboundsSkipsStaleSnapshot covers #1055/#1020: the
// migrate-legacy snapshot the handler iterates must come from inside the
// mutation lock, and every entry is re-checked against live state — a
// caller-held snapshot that still lists a just-deleted inbound must not
// mint a client/binding for it.
func TestMigrateLegacyInboundsSkipsStaleSnapshot(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	// Live desired state: only "kept" exists; "gone" was deleted after a
	// hypothetical stale snapshot was taken.
	s.inbounds = []Inbound{{Name: "kept", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	s.clientRepo = repo
	s.clientMigrator = client.NewMigrator(repo, client.NewCredentialStore(db, s.cipher))

	stale := []Inbound{
		{Name: "gone", Protocol: "hysteria2", Enabled: true, Profiles: []ClientProfile{
			{Username: "alice", Password: "pw", Enabled: true},
		}},
		{Name: "kept", Protocol: "hysteria2", Enabled: true, Profiles: []ClientProfile{
			{Username: "bob", Password: "pw", Enabled: true},
		}},
	}
	var results []inboundMigrateResult
	var applied bool
	err := repo.WithTx(func(tx *client.Tx) error {
		var err error
		results, applied, err = s.migrateLegacyInboundsLocked(tx, stale)
		return err
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !applied {
		t.Fatal("kept inbound profiles should migrate")
	}
	if len(results) != 1 || results[0].Inbound != "kept" {
		t.Fatalf("results = %+v, want only the live inbound", results)
	}
	clients, total, err := repo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 1 {
		t.Fatalf("deleted inbound was resurrected: %d clients (%+v)", total, clients)
	}
	bindings, err := repo.BindingsForClient(clients[0].ID)
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].InboundID != "kept" {
		t.Fatalf("bindings = %+v, want one binding to kept", bindings)
	}
}

// TestMigrateLegacyAuditRecordsFailure covers the audit-honesty half of
// #1055: the old handler logged migrate_legacy success BEFORE the mutation
// ran, so a failed commit still produced a success record. The audit entry
// must now reflect the actual outcome.
func TestMigrateLegacyAuditRecordsFailure(t *testing.T) {
	dir := t.TempDir()
	recorder := audit.NewRecorder(filepath.Join(dir, "audit.jsonl"), audit.RecorderOptions{})
	s := &managementState{audit: recorder}
	// Migrator wired but no client repository: the mutation fails before it
	// starts, which is enough to prove the audit follows the outcome.
	db := openApplyTestDB(t)
	s.clientMigrator = client.NewMigrator(client.NewRepository(db), client.NewCredentialStore(db, newTestCipher(t)))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients/migrate-legacy", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleV1MigrateLegacy(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("migrate-legacy status = %d, want 500", rec.Code)
	}
	records := auditActionsFor(t, recorder, "migrate_legacy")
	if len(records) != 1 || records[0].Success {
		t.Fatalf("migrate_legacy audit records = %+v, want exactly one failure", records)
	}
}
