package client

import (
	"fmt"
	"testing"
)

func TestCredentialsForInboundIncludesClientsBeyondDefaultPage(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	oldest, err := repo.Create(Client{Name: "oldest-bound", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: oldest.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set(binding.ID, "password", "oldest-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clients SET created_at=1 WHERE id=?`, oldest.ID); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 25; i++ {
		newer, err := repo.Create(Client{Name: fmt.Sprintf("newer-%02d", i), Enabled: true, QuotaResetPolicy: ResetNever})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE clients SET created_at=? WHERE id=?`, int64(100+i), newer.ID); err != nil {
			t.Fatal(err)
		}
	}

	page, total, err := repo.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 26 {
		t.Fatalf("total=%d, want 26", total)
	}
	if len(page) != 25 {
		t.Fatalf("default page size=%d, want 25", len(page))
	}
	for _, item := range page {
		if item.ID == oldest.ID {
			t.Fatal("oldest client unexpectedly present on the default list page")
		}
	}

	creds, err := svc.CredentialsForInbound("hy")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Name != "oldest-bound" || creds[0].Password != "oldest-secret" {
		t.Fatalf("credentials=%+v", creds)
	}
}

func TestCredentialsForInboundReturnsEveryBoundClientOnSameInbound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	for i := 0; i < 26; i++ {
		row, err := repo.Create(Client{Name: fmt.Sprintf("bound-%02d", i), Enabled: true, QuotaResetPolicy: ResetNever})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Set(binding.ID, "password", fmt.Sprintf("secret-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	creds, err := svc.CredentialsForInbound("hy")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 26 {
		t.Fatalf("got %d credentials, want 26", len(creds))
	}
}
