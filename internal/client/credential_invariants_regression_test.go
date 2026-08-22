package client

import (
	"database/sql"
	"strings"
	"testing"
)

func TestDatabaseRejectsDuplicateActiveCredentialPerBindingAndKind(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	cipher := newTestCipher(t)
	store := NewCredentialStore(db, cipher)
	row, err := repo.Create(Client{Name: "credential-unique", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Set(binding.ID, "password", "first")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO client_credentials
  (id,binding_id,kind,encrypted_value,key_version,credential_version,created_at,revoked_at)
  VALUES('duplicate-active',?,?,?,?,?,?,NULL)`, binding.ID, "password", first.EncryptedValue, 1, 2, first.CreatedAt+1)
	if err == nil {
		t.Fatal("database accepted a second non-revoked credential for the same binding and kind")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate active credential failed for the wrong reason: %v", err)
	}
}

func TestSetCredentialAtomicallyReplacesExistingActiveCredential(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	row, err := repo.Create(Client{Name: "credential-set", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Set(binding.ID, "password", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Set(binding.ID, "password", "second")
	if err != nil {
		t.Fatalf("set should atomically behave as rotate when active material exists: %v", err)
	}
	if second.CredentialVersion != first.CredentialVersion+1 {
		t.Fatalf("replacement version=%d, want %d", second.CredentialVersion, first.CredentialVersion+1)
	}
	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM client_credentials WHERE binding_id=? AND kind='password' AND revoked_at IS NULL`, binding.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active password credentials=%d, want exactly one", active)
	}
	old, err := store.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.RevokedAt == nil {
		t.Fatal("set replacement left the previous credential active")
	}
	plain, err := store.Reveal(second.ID)
	if err != nil || plain != "second" {
		t.Fatalf("replacement plaintext=%q err=%v", plain, err)
	}
}

func TestCredentialLookupFailsClosedOnAmbiguousActiveRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	row, _ := repo.Create(Client{Name: "credential-ambiguous", Enabled: true, QuotaResetPolicy: ResetNever})
	binding, _ := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	first, err := store.Set(binding.ID, "password", "first")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-repair database by removing only the new partial index.
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_credentials_one_active_kind`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO client_credentials
  (id,binding_id,kind,encrypted_value,key_version,credential_version,created_at)
  VALUES('ambiguous-active',?,?,?,?,?,?)`, binding.ID, "password", first.EncryptedValue, 1, 2, first.CreatedAt+1); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			t.Fatal(err)
		}
		// On schemas where another equivalent index exists, the invariant is
		// already enforced and ambiguity cannot be manufactured.
		return
	}
	if _, err := store.ActiveForBinding(binding.ID, "password"); err == nil || err == sql.ErrNoRows {
		t.Fatalf("ambiguous active rows did not fail closed: %v", err)
	}
}

func TestMutableRuntimeRendererExcludesExpiredClient(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	service := NewService(repo, store)
	expired := int64(100)
	row, err := repo.Create(Client{Name: "expired-runtime", Enabled: true, ExpiresAt: &expired, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set(binding.ID, "password", "expired-secret"); err != nil {
		t.Fatal(err)
	}
	originalNow := nowUnix
	nowUnix = func() int64 { return 101 }
	t.Cleanup(func() { nowUnix = originalNow })
	credentials, err := service.CredentialsForInbound("hy")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 0 {
		t.Fatalf("mutable runtime renderer emitted expired client credentials: %+v", credentials)
	}
}
