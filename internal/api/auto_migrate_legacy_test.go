package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// TestAutoMigrateLegacyOnReload verifies (A6) that legacy inbound-embedded
// profiles are automatically migrated to normalized Client+Binding+Credential
// on state reload (startup/upgrade), not just via the manual API button.
func TestAutoMigrateLegacyOnReload(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example"}
	// Legacy inbound with embedded profiles.
	s.inbounds = []Inbound{{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		Profiles: []ClientProfile{
			{Name: "legacy-alice", Username: "alice", Password: "alice-pass", Enabled: true},
			{Name: "legacy-bob", Username: "bob", Password: "bob-pass", Enabled: true},
		},
	}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	s.clientService = client.NewService(repo, creds, nil)
	s.clientRepo = repo
	s.clientMigrator = client.NewMigrator(repo, creds)

	// Run auto-migration (simulates ReloadLocked).
	lifecycle := NewManagementStateLifecycle(s)
	if err := lifecycle.AutoMigrateLegacyLocked(); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}

	// Verify clients were created.
	clients, total, err := repo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 migrated clients, got %d", total)
	}

	// Verify bindings and credentials.
	for _, c := range clients {
		bindings, err := repo.BindingsForClient(c.ID)
		if err != nil {
			t.Fatalf("bindings for %s: %v", c.ID, err)
		}
		if len(bindings) != 1 {
			t.Errorf("client %s: expected 1 binding, got %d", c.Name, len(bindings))
			continue
		}
		if bindings[0].InboundID != "hy2" {
			t.Errorf("client %s: binding inbound %q, want hy2", c.Name, bindings[0].InboundID)
		}
		credsList, err := creds.ListForBinding(bindings[0].ID)
		if err != nil {
			t.Fatalf("creds for binding %s: %v", bindings[0].ID, err)
		}
		if len(credsList) == 0 {
			t.Errorf("client %s: no credentials migrated", c.Name)
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
	s.clientMigrator = client.NewMigrator(repo, creds)

	lifecycle := NewManagementStateLifecycle(s)
	if err := lifecycle.AutoMigrateLegacyLocked(); err != nil {
		t.Fatalf("auto-migrate with no profiles: %v", err)
	}

	_, total, _ := repo.List(client.ListFilter{})
	if total != 0 {
		t.Errorf("expected 0 clients, got %d", total)
	}
}
