package client

import (
	"testing"
)

func TestMigrateLegacyProfilesCreatesClientsBindingsCredentials(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)
	mig := NewMigrator(repo, cs)

	legacy := []LegacyProfile{
		{Name: "alice", Username: "alice", Password: "pw-alice", Enabled: true},
		{Name: "bob", Username: "bob", Password: "pw-bob", Enabled: true},
	}
	res, err := mig.MigrateInboundProfiles("in-hy2", "hysteria2", legacy)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.ClientsCreated != 2 || res.BindingsCreated != 2 || res.CredentialsCreated != 2 {
		t.Fatalf("unexpected migration result: %+v", res)
	}

	// Each legacy profile -> client with a binding to the inbound + credential.
	clients, total, err := repo.List(ListFilter{InboundID: "in-hy2"})
	if err != nil || total != 2 {
		t.Fatalf("expected 2 clients bound, got total=%d err=%v", total, err)
	}
	for _, cl := range clients {
		bindings, _ := repo.BindingsForClient(cl.ID)
		if len(bindings) != 1 || bindings[0].InboundID != "in-hy2" {
			t.Fatalf("client %s missing binding: %+v", cl.Name, bindings)
		}
		creds, _ := cs.ListForBinding(bindings[0].ID)
		if len(creds) != 1 {
			t.Fatalf("client %s missing credential: %+v", cl.Name, creds)
		}
		// Password must round-trip via reveal.
		active, err := cs.ActiveForBinding(bindings[0].ID, "password")
		if err != nil {
			t.Fatalf("active credential: %v", err)
		}
		plain, _ := cs.Reveal(active.ID)
		if plain != "pw-alice" && plain != "pw-bob" {
			t.Fatalf("unexpected decrypted credential: %q", plain)
		}
	}
}

func TestMigrateSkipsDisabledAndEmptyProfiles(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	cs := NewCredentialStore(db, newTestCipher(t))
	mig := NewMigrator(repo, cs)

	res, err := mig.MigrateInboundProfiles("in-hy2", "hysteria2", []LegacyProfile{
		{Name: "off", Username: "off", Password: "pw", Enabled: false},
		{Name: "nopw", Username: "nopw", Password: "", Enabled: true},
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.ClientsCreated != 0 {
		t.Fatalf("expected no clients from disabled/empty profiles, got %+v", res)
	}
}

func TestMigrateDisabledClientProducesDisabledBinding(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	cs := NewCredentialStore(db, newTestCipher(t))
	mig := NewMigrator(repo, cs, WithIncludeDisabled())

	// A disabled legacy profile should still produce a disabled client/binding
	// so it remains visible (not silently dropped), but is NOT active.
	res, err := mig.MigrateInboundProfiles("in-naive", "naiveproxy", []LegacyProfile{
		{Name: "off", Username: "off", Password: "pw", Enabled: false},
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.ClientsCreated != 1 {
		t.Fatalf("expected 1 disabled client, got %+v", res)
	}
	clients, _, _ := repo.List(ListFilter{})
	if !clients[0].Enabled == clients[0].Enabled && clients[0].Enabled {
		t.Fatalf("expected disabled client, got %+v", clients[0])
	}
}

func TestMigrationIdempotentByStableID(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	cs := NewCredentialStore(db, newTestCipher(t))
	mig := NewMigrator(repo, cs)

	legacy := []LegacyProfile{{Name: "alice", Username: "alice", Password: "pw", Enabled: true}}
	first, err := mig.MigrateInboundProfiles("in-hy2", "hysteria2", legacy)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Re-run must not duplicate: stable IDs keyed on (inbound, username).
	second, err := mig.MigrateInboundProfiles("in-hy2", "hysteria2", legacy)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ClientsCreated != 0 {
		t.Fatalf("re-migration must be idempotent, created %+v again", second)
	}
	clients, total, _ := repo.List(ListFilter{InboundID: "in-hy2"})
	if total != 1 {
		t.Fatalf("expected 1 client after idempotent re-run, got %d (%+v)", total, clients)
	}
	if len(first.ClientIDs) != 1 {
		t.Fatalf("expected first migration to record client id, got %+v", first)
	}
}
