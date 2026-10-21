package client

import (
	"strings"
	"testing"
)

// #1283 follow-up: master-key rotation decrypted with the legacy plaintext
// passthrough and re-encrypted under the new key, so a planted non-"ve1:" row
// came out of rotation as a real ciphertext that the hardened reveal paths
// then accepted. Rotation must refuse such rows instead of laundering them.

func TestReencryptSubscriptionTokensRefusesPlantedPlaintext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	oldCipher, newCipher := newTestCipher(t), newTestCipher(t)
	ts := NewTokenStore(db).WithCipher(oldCipher)
	t.Cleanup(ts.Close)
	c, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	issued, err := ts.Issue(c.ID, "phone", nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	for _, planted := range []string{"attacker-chosen-token", "VE1:abc", "ve1:"} {
		if _, err := db.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, []byte(planted), issued.Token.ID); err != nil {
			t.Fatalf("plant: %v", err)
		}
		tx, err := repo.BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		rotateErr := tx.ReencryptSubscriptionTokens(oldCipher, newCipher)
		if rotateErr == nil {
			_ = tx.Commit()
			rotated := NewTokenStore(db).WithCipher(newCipher)
			got, revealErr := rotated.Reveal(issued.Token.ID)
			rotated.Close()
			t.Fatalf("rotation accepted planted %q; post-rotation Reveal = %q, %v", planted, got, revealErr)
		}
		_ = tx.Rollback()
		if !strings.Contains(rotateErr.Error(), "ciphertext is invalid") {
			t.Fatalf("rotation of planted %q failed for an unrelated reason: %v", planted, rotateErr)
		}
	}
}

func TestReencryptCredentialsRefusesPlantedPlaintext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	oldCipher, newCipher := newTestCipher(t), newTestCipher(t)
	store := NewCredentialStore(db, oldCipher)
	c, err := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := store.Set(binding.ID, "password", "real-secret")
	if err != nil {
		t.Fatal(err)
	}

	// The >12-byte length rule is a migration-time check, not a CHECK
	// constraint, so a bare "ve1:" row can be planted directly too.
	for _, planted := range []string{"attacker-chosen-password", "VE1:attacker-chosen", "ve1:"} {
		if _, err := db.Exec(`UPDATE client_credentials SET encrypted_value=? WHERE id=?`, []byte(planted), cred.ID); err != nil {
			t.Fatalf("plant: %v", err)
		}
		tx, err := repo.BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		rotateErr := tx.ReencryptCredentials(oldCipher, newCipher)
		if rotateErr == nil {
			_ = tx.Commit()
			got, revealErr := NewCredentialStore(db, newCipher).Reveal(cred.ID)
			t.Fatalf("rotation accepted planted %q; post-rotation Reveal = %q, %v", planted, got, revealErr)
		}
		_ = tx.Rollback()
		if !strings.Contains(rotateErr.Error(), "ciphertext is invalid") {
			t.Fatalf("rotation of planted %q failed for an unrelated reason: %v", planted, rotateErr)
		}
	}
}

func TestReencryptCredentialsStillRotatesValidCiphertext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	oldCipher, newCipher := newTestCipher(t), newTestCipher(t)
	c, err := repo.Create(Client{Name: "carol", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := NewCredentialStore(db, oldCipher).Set(binding.ID, "password", "real-secret")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.ReencryptCredentials(oldCipher, newCipher); err != nil {
		_ = tx.Rollback()
		t.Fatalf("rotate valid credential: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := NewCredentialStore(db, newCipher).Reveal(cred.ID)
	if err != nil || got != "real-secret" {
		t.Fatalf("Reveal after rotation = %q, %v", got, err)
	}
}

// CredentialStore.Reveal used strings.HasPrefix("ve1:"), so a bare "ve1:" value
// slipped past it and Cipher.Decrypt then echoed it back verbatim.
func TestCredentialRevealRejectsBarePrefix(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	c, err := repo.Create(Client{Name: "dave", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := store.Set(binding.ID, "password", "real-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE client_credentials SET encrypted_value=? WHERE id=?`, []byte("ve1:"), cred.ID); err != nil {
		t.Fatalf("plant: %v", err)
	}
	if got, err := store.Reveal(cred.ID); err == nil {
		t.Fatalf("Reveal of bare ve1: = %q, want error", got)
	}
}

// Rollback restore must not re-insert a non-"ve1:" snapshot credential as a
// live row: Reveal would refuse it and every later rotation would fail closed.
func TestReplaceSnapshotRefusesPlaintextCredential(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	c, err := repo.Create(Client{Name: "erin", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planted := Credential{
		ID: "planted-credential", BindingID: binding.ID, Kind: "password",
		EncryptedValue: []byte("attacker-chosen-password"), KeyVersion: 1, CredentialVersion: 1,
	}
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	err = ReplaceSnapshotTx(tx, []Client{c}, []Binding{binding}, []Credential{planted})
	_ = tx.Rollback()
	if err == nil || !strings.Contains(err.Error(), "ciphertext is invalid") {
		t.Fatalf("ReplaceSnapshotTx with plaintext credential err=%v, want ciphertext is invalid", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM client_credentials WHERE id=?`, planted.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("planted credential rows=%d err=%v", count, err)
	}
}

// The refusal message is an operator runbook: following it must actually let
// rotation succeed. For tokens, revoking is a remedy since #1346 (revocation
// clears token_ciphertext); a row that is already revoked but still holds a
// stale ciphertext is still rotated and must be cleared by hand.
func TestRotationRefusalHintsUnstickRotation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	oldCipher, newCipher := newTestCipher(t), newTestCipher(t)
	ts := NewTokenStore(db).WithCipher(oldCipher)
	t.Cleanup(ts.Close)
	creds := NewCredentialStore(db, oldCipher)
	c, err := repo.Create(Client{Name: "frank", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := ts.Issue(c.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := creds.Set(binding.ID, "password", "real-secret")
	if err != nil {
		t.Fatal(err)
	}
	rotate := func(fn func(*Tx) error) error {
		tx, err := repo.BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		rotateErr := fn(tx)
		_ = tx.Rollback()
		return rotateErr
	}
	tokens := func(tx *Tx) error { return tx.ReencryptSubscriptionTokens(oldCipher, newCipher) }
	credentials := func(tx *Tx) error { return tx.ReencryptCredentials(oldCipher, newCipher) }

	if _, err := db.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, "attacker-chosen-token", issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	err = rotate(tokens)
	if err == nil || !strings.Contains(err.Error(), "ciphertext is invalid") {
		t.Fatalf("planted token: rotation err=%v, want refusal", err)
	}
	if !strings.Contains(err.Error(), "revoke it, which also clears its token_ciphertext") {
		t.Fatalf("token refusal must offer revocation as the remedy: %v", err)
	}
	if err := ts.Revoke(issued.Token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := rotate(tokens); err != nil {
		t.Fatalf("token rotation after revoking as hinted: %v", err)
	}

	// A row revoked by an older build (revoked_at set, ciphertext kept) is
	// still rotated; the hint's hand-clear step must unstick it.
	stale, err := ts.Issue(c.ID, "stale", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE subscription_tokens SET token_ciphertext=?, revoked_at=? WHERE id=?`, "attacker-chosen-token", nowUnix(), stale.Token.ID); err != nil {
		t.Fatal(err)
	}
	err = rotate(tokens)
	if err == nil || !strings.Contains(err.Error(), "needs its token_ciphertext cleared by hand") {
		t.Fatalf("stale revoked token: rotation err=%v, want refusal with the hand-clear hint", err)
	}
	if err := ts.Revoke(stale.Token.ID); err == nil {
		t.Fatal("revoking an already-revoked row must not report success")
	}
	if _, err := db.Exec(`UPDATE subscription_tokens SET token_ciphertext='' WHERE id=?`, stale.Token.ID); err != nil {
		t.Fatal(err)
	}
	if err := rotate(tokens); err != nil {
		t.Fatalf("token rotation after clearing token_ciphertext as hinted: %v", err)
	}

	// Plant into the binding's active credential: the hint says to rotate it
	// first so deleting the planted row cannot leave an enabled binding with
	// no active credential (which would fail every render).
	if _, err := db.Exec(`UPDATE client_credentials SET encrypted_value=? WHERE id=?`, []byte("attacker-chosen-password"), cred.ID); err != nil {
		t.Fatal(err)
	}
	if active, err := creds.ActiveForBinding(binding.ID, "password"); err != nil || active.ID != cred.ID {
		t.Fatalf("planted row is not the active credential: %+v, %v", active, err)
	}
	if !strings.Contains(func() string {
		err := rotate(credentials)
		if err == nil {
			t.Fatal("rotation accepted planted active credential")
		}
		return err.Error()
	}(), "rotate it first") {
		t.Fatal("credential refusal does not tell the operator to rotate an active credential before deleting it")
	}
	replacement, err := creds.Rotate(binding.ID, "password", "new-secret")
	if err != nil {
		t.Fatalf("credential rotate: %v", err)
	}
	err = rotate(credentials)
	if err == nil || !strings.Contains(err.Error(), "ciphertext is invalid") || !strings.Contains(err.Error(), cred.ID) {
		t.Fatalf("rotated-away planted credential: rotation err=%v, want refusal on %s (rotate must not be a remedy)", err, cred.ID)
	}
	if strings.Contains(err.Error(), "rotate or revoke the credential") {
		t.Fatalf("credential refusal still recommends rotate/revoke: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM client_credentials WHERE id=?`, cred.ID); err != nil {
		t.Fatal(err)
	}
	if err := rotate(credentials); err != nil {
		t.Fatalf("credential rotation after deleting the row as hinted: %v", err)
	}
	active, err := creds.ActiveForBinding(binding.ID, "password")
	if err != nil || active.ID != replacement.ID {
		t.Fatalf("binding lost its active credential after following the hint: %+v, %v", active, err)
	}
	if got, err := creds.Reveal(active.ID); err != nil || got != "new-secret" {
		t.Fatalf("Reveal(active) = %q, %v", got, err)
	}
}
