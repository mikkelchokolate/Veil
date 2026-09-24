package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// TestAutoMigrateLegacyOnReload verifies (A6) that legacy inbound-embedded
// profiles are automatically migrated to normalized Client+Binding+Credential
// on state reload (startup/upgrade), not just via the manual API button. It
// drives the real ReloadLocked wiring — load persisted state, rebuild the
// client subsystem, run the migration — so dropping the migration call from
// ReloadLocked fails this test.
func TestAutoMigrateLegacyOnReload(t *testing.T) {
	dir := t.TempDir()
	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(dir, "state.json"),
		KeyPath:   filepath.Join(dir, "state.key"),
		ApplyRoot: filepath.Join(dir, "apply"),
		Mode:      "dev",
	})
	defer closeClientSubsystem(state)

	state.settings.Domain = "x.example"
	// Legacy inbound with embedded profiles, persisted to state.json.
	state.inbounds = []Inbound{{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		Profiles: []ClientProfile{
			{Name: "legacy-alice", Username: "alice", Password: "alice-pass", Enabled: true},
			{Name: "legacy-bob", Username: "bob", Password: "bob-pass", Enabled: true},
		},
	}}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.SaveLocked(); err != nil {
		t.Fatalf("SaveLocked: %v", err)
	}
	// Drop the in-memory copy so the reload must repopulate it from disk.
	state.inbounds = nil

	if err := lifecycle.ReloadLocked(); err != nil {
		t.Fatalf("ReloadLocked: %v", err)
	}
	if len(state.inbounds) != 1 || len(state.inbounds[0].Profiles) != 2 {
		t.Fatalf("reload did not restore legacy inbounds: %+v", state.inbounds)
	}

	repo := state.clientRepo
	if repo == nil {
		t.Fatal("client repo not wired after reload")
	}
	clients, total, err := repo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 migrated clients after reload, got %d", total)
	}

	// Verify bindings and credential material survive the reload-driven
	// migration: each migrated credential must reveal the legacy password.
	creds := client.NewCredentialStore(state.db, state.cipher)
	// Migrated clients take Name from the legacy profile record
	// ("legacy-alice"), not the embedded credential username.
	wantPasswords := map[string]string{"legacy-alice": "alice-pass", "legacy-bob": "bob-pass"}
	for _, c := range clients {
		bindings, err := repo.BindingsForClient(c.ID)
		if err != nil {
			t.Fatalf("bindings for %s: %v", c.ID, err)
		}
		if len(bindings) != 1 {
			t.Fatalf("client %s: expected 1 binding, got %d", c.Name, len(bindings))
		}
		if bindings[0].InboundID != "hy2" {
			t.Fatalf("client %s: binding inbound %q, want hy2", c.Name, bindings[0].InboundID)
		}
		credsList, err := creds.ListForBinding(bindings[0].ID)
		if err != nil {
			t.Fatalf("creds for binding %s: %v", bindings[0].ID, err)
		}
		if len(credsList) == 0 {
			t.Fatalf("client %s: no credentials migrated", c.Name)
		}
		revealed, err := creds.Reveal(credsList[0].ID)
		if err != nil {
			t.Fatalf("reveal credential for %s: %v", c.Name, err)
		}
		want, ok := wantPasswords[c.Name]
		if !ok {
			t.Fatalf("unexpected migrated client %q", c.Name)
		}
		if revealed != want {
			t.Fatalf("client %s credential = %q, want %q", c.Name, revealed, want)
		}
	}
}

// TestAutoMigrateLegacyIdempotent verifies that re-running auto-migration
// does not duplicate clients (idempotent by stable derived ID).
func TestAutoMigrateLegacyIdempotent(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.inbounds = []Inbound{{
		Name: "hy2", Protocol: "hysteria2", Enabled: true,
		Profiles: []ClientProfile{
			{Username: "alice", Password: "pass", Enabled: true},
		},
	}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	s.clientRepo = repo
	s.clientMigrator = client.NewMigrator(repo, creds)

	lifecycle := NewManagementStateLifecycle(s)

	// Run twice.
	if err := lifecycle.AutoMigrateLegacyLocked(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := lifecycle.AutoMigrateLegacyLocked(); err != nil {
		t.Fatalf("second run: %v", err)
	}

	_, total, err := repo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 client after idempotent re-run, got %d", total)
	}
}

// TestAutoMigrateLegacySkipsEmptyProfiles verifies auto-migration handles
// inbounds with no profiles gracefully.
func TestAutoMigrateLegacySkipsEmptyProfiles(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.inbounds = []Inbound{
		{Name: "hy2", Protocol: "hysteria2", Enabled: true}, // no profiles
	}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	s.clientRepo = repo
	s.clientMigrator = client.NewMigrator(repo, creds)

	lifecycle := NewManagementStateLifecycle(s)
	if err := lifecycle.AutoMigrateLegacyLocked(); err != nil {
		t.Fatalf("auto-migrate with no profiles: %v", err)
	}

	_, total, err := repo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 clients, got %d", total)
	}
}
