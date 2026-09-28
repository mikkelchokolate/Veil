package client

import (
	"testing"
)

// TestReplaceSnapshotTxNeverResurrectsRevokedCredentials locks the #1099
// credential contract at the persistence layer: a snapshot credential that
// was rotated out (revoked tombstone present) must stay revoked, the rotated
// successor must remain the only active credential, and the insert must never
// violate the one-active-per-binding-kind index.
func TestReplaceSnapshotTxNeverResurrectsRevokedCredentials(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	row, err := repo.Create(Client{Name: "rotated", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Set(binding.ID, "password", "snapshot-secret")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Rotate(binding.ID, "password", "rotated-secret")
	if err != nil {
		t.Fatal(err)
	}

	// The immutable snapshot still carries the rotated-out credential row.
	snapshotCred := Credential{
		ID: old.ID, BindingID: binding.ID, Kind: "password",
		EncryptedValue: old.EncryptedValue, KeyVersion: old.KeyVersion,
		CredentialVersion: old.CredentialVersion, CreatedAt: old.CreatedAt,
	}
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{row}, []Binding{binding}, []Credential{snapshotCred}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	restored, err := store.Get(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.RevokedAt == nil {
		t.Fatal("rollback un-revoked the rotated-out credential tombstone")
	}
	active, err := store.ActiveForBinding(binding.ID, "password")
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != rotated.ID {
		t.Fatalf("active credential %s, want rotated %s", active.ID, rotated.ID)
	}
	plain, err := store.Reveal(active.ID)
	if err != nil || plain != "rotated-secret" {
		t.Fatalf("active plaintext=%q err=%v, want rotated-secret", plain, err)
	}
}

// TestReplaceSnapshotTxKeepsPostSnapshotDeletionsAndRestrictions locks the
// entity-level half of #1099: a client deleted after the snapshot is not
// resurrected, and a post-snapshot disable/depletion survives the rollback
// even when the snapshot recorded an enabled, undepleted row.
func TestReplaceSnapshotTxKeepsPostSnapshotDeletionsAndRestrictions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	deleted, err := repo.Create(Client{Name: "deleted", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := repo.Create(Client{Name: "disabled", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(deleted.ID); err != nil {
		t.Fatal(err)
	}
	turnedOff, err := repo.Update(Client{
		ID: disabled.ID, Name: disabled.Name, Enabled: false, Depleted: true,
		QuotaResetPolicy: ResetNever,
	}, disabled.Version)
	if err != nil {
		t.Fatal(err)
	}

	// The snapshot predates both events: the deleted client is still present
	// and the restricted client is recorded enabled and undepleted.
	snapshotDeleted := deleted
	snapshotDisabled := disabled // snapshot: enabled, not depleted
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{snapshotDeleted, snapshotDisabled}, nil, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(deleted.ID); err == nil {
		t.Fatal("rollback resurrected a client deleted after the snapshot")
	}
	got, err := repo.Get(disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("rollback re-enabled a client disabled after the snapshot")
	}
	if !got.Depleted {
		t.Fatal("rollback cleared a depletion recorded after the snapshot")
	}
	if got.Version <= turnedOff.Version {
		t.Fatalf("restored version %d not above live %d", got.Version, turnedOff.Version)
	}
}
