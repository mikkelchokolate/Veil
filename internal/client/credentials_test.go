package client

import (
	"testing"
)

func TestCredentialStoreEncryptsAtRest(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy2", Enabled: true})

	cred, err := cs.Set(b.ID, "password", "super-secret-value")
	if err != nil {
		t.Fatalf("set credential: %v", err)
	}
	if cred.Kind != "password" || cred.CredentialVersion != 1 {
		t.Fatalf("unexpected credential: %+v", cred)
	}

	// The raw stored value must NOT be plaintext.
	var raw []byte
	if err := db.QueryRow(`SELECT encrypted_value FROM client_credentials WHERE id=?`, cred.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if string(raw) == "super-secret-value" || contains(string(raw), "super-secret") {
		t.Fatalf("credential stored in plaintext: %q", string(raw))
	}

	// Decrypting must round-trip.
	plain, err := cs.Reveal(cred.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if plain != "super-secret-value" {
		t.Fatalf("round-trip mismatch: %q", plain)
	}
}

func TestCredentialRotationKeepsVersions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy2", Enabled: true})

	v1, _ := cs.Set(b.ID, "password", "old-value")
	rotated, err := cs.Rotate(b.ID, "password", "new-value")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.CredentialVersion != v1.CredentialVersion+1 {
		t.Fatalf("expected version bump, got %d", rotated.CredentialVersion)
	}
	// Old version should be revoked.
	old, err := cs.Get(v1.ID)
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if old.RevokedAt == nil {
		t.Fatalf("expected old credential revoked after rotation")
	}
	// Active lookup returns the new version.
	active, err := cs.ActiveForBinding(b.ID, "password")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	plain, _ := cs.Reveal(active.ID)
	if plain != "new-value" {
		t.Fatalf("active credential is not the new value: %q", plain)
	}
}

func TestCredentialListOmitsPlaintext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy2", Enabled: true})
	_, _ = cs.Set(b.ID, "password", "hidden")

	creds, err := cs.ListForBinding(b.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	if len(creds[0].EncryptedValue) != 0 {
		t.Fatalf("list must not return encrypted material")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
