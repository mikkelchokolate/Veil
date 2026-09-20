package client

import (
	"errors"
	"testing"
)

// TestCredentialWriteNormalizesOnce is the #334 regression: operator-supplied
// credential plaintext could be stored with surrounding whitespace while the
// Caddy forward_auth renderer trimmed it, so the server authenticated a
// different secret than the export advertised. Every write path now trims
// once at the storage boundary so the stored bytes are the canonical bytes.
func TestCredentialWriteNormalizesOnce(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	row, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SetCredential(binding.ID, "password", " padded-secret "); err != nil {
		t.Fatalf("set credential: %v", err)
	}
	creds, err := svc.CredentialsForInbound("hy")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Password != "padded-secret" {
		t.Fatalf("credentials=%+v, want trimmed stored bytes", creds)
	}

	if _, err := svc.RotateCredential(binding.ID, "password", " rotated-secret "); err != nil {
		t.Fatalf("rotate credential: %v", err)
	}
	creds, err = svc.CredentialsForInbound("hy")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Password != "rotated-secret" {
		t.Fatalf("credentials=%+v, want trimmed rotated bytes", creds)
	}
}

// TestCredentialWriteRejectsWhitespaceOnly pins the validation side of the
// same contract: a value that trims to empty is rejected as missing rather
// than stored as an unusable credential.
func TestCredentialWriteRejectsWhitespaceOnly(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	row, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(binding.ID, "password", "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("set whitespace-only credential = %v, want ErrValidation", err)
	}
}
