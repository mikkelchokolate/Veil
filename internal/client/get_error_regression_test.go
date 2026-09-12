package client

import (
	"errors"
	"testing"
)

func TestServiceGetPreservesStorageErrors(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	created, err := repo.Create(Client{Name: "storage-err", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get("missing-client"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing client: got %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(created.ID); err != nil {
		t.Fatalf("existing client: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Get(created.ID)
	if err == nil {
		t.Fatal("closed database returned success")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("storage failure mapped to ErrNotFound: %v", err)
	}
}
