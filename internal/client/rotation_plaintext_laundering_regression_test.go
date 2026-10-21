package client

import (
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

	// Length > 12 so the planted rows also satisfy the migration-time sanity
	// check; the bare-prefix case is covered by the Reveal test below.
	for _, planted := range []string{"attacker-chosen-password", "VE1:attacker-chosen"} {
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
