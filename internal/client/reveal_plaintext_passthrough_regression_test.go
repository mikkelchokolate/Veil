package client

import (
	"testing"
)

// #1283: Cipher.Decrypt passes non-"ve1:" input through verbatim. The snapshot
// reveal path and the token reveal path must refuse it instead of echoing
// attacker-chosen bytes as a decrypted secret.

func TestRevealEncryptedRejectsPlaintextPassthrough(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	cs := NewCredentialStore(db, cipher)

	for _, planted := range []string{"attacker-chosen-password", "VE1:abc", "ve1:"} {
		if got, err := cs.RevealEncrypted([]byte(planted)); err == nil {
			t.Fatalf("RevealEncrypted(%q) = %q, want error", planted, got)
		}
	}

	encrypted, err := cipher.Encrypt("real-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := cs.RevealEncrypted([]byte(encrypted))
	if err != nil || got != "real-secret" {
		t.Fatalf("RevealEncrypted(valid) = %q, %v", got, err)
	}
}

func TestTokenRevealRejectsPlantedPlaintextCiphertext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db).WithCipher(newTestCipher(t))
	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	issued, err := ts.Issue(c.ID, "phone", nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := db.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, "attacker-chosen-token", issued.Token.ID); err != nil {
		t.Fatalf("plant: %v", err)
	}
	if got, err := ts.Reveal(issued.Token.ID); err == nil {
		t.Fatalf("Reveal of planted plaintext = %q, want error", got)
	}
}
