package client

import "testing"

// #1272 item 6: when a snapshot moves a binding back to another client, the
// restore deletes and re-inserts the binding row (#1140). The delete used to
// cascade into client_credentials, so restoreSnapshotCredentials then
// installed the older snapshot credential as the only active one: a rollback
// silently undid a post-snapshot rotation and dropped its tombstones. The
// live credential rows must survive the reparent unchanged.
func TestReplaceSnapshotTxReparentKeepsLiveCredentials(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	owner, err := repo.Create(Client{Name: "snapshot-owner", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.Create(Client{Name: "live-owner", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: owner.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Set(binding.ID, "password", "snapshot-secret")
	if err != nil {
		t.Fatal(err)
	}
	snapshotCred := Credential{
		ID: old.ID, BindingID: binding.ID, Kind: "password",
		EncryptedValue: old.EncryptedValue, KeyVersion: old.KeyVersion,
		CredentialVersion: old.CredentialVersion, CreatedAt: old.CreatedAt,
	}

	// Post-snapshot: an out-of-band write moves the binding to another client,
	// then the credential is rotated.
	if _, err := db.Exec(`UPDATE client_bindings SET client_id=? WHERE id=?`, other.ID, binding.ID); err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Rotate(binding.ID, "password", "rotated-secret")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{owner, other}, []Binding{binding}, []Credential{snapshotCred}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	restored, err := repo.GetBinding(binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ClientID != owner.ID {
		t.Fatalf("binding client=%s, want snapshot owner %s", restored.ClientID, owner.ID)
	}
	active, err := store.ActiveForBinding(binding.ID, "password")
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != rotated.ID {
		t.Fatalf("active credential %s, want the post-snapshot rotation %s", active.ID, rotated.ID)
	}
	if active.CredentialVersion != rotated.CredentialVersion || active.CreatedAt != rotated.CreatedAt {
		t.Fatalf("active credential metadata changed: version=%d created=%d, want %d/%d",
			active.CredentialVersion, active.CreatedAt, rotated.CredentialVersion, rotated.CreatedAt)
	}
	plain, err := store.Reveal(active.ID)
	if err != nil || plain != "rotated-secret" {
		t.Fatalf("active plaintext=%q err=%v, want rotated-secret", plain, err)
	}
	tomb, err := store.Get(old.ID)
	if err != nil {
		t.Fatalf("rotated-out tombstone lost: %v", err)
	}
	if tomb.RevokedAt == nil {
		t.Fatal("rollback re-activated the rotated-out snapshot credential")
	}
}
