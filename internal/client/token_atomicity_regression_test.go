package client

import (
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestConcurrentRevokeWinsLookupAndLastUsedTransaction(t *testing.T) {
	path := t.TempDir() + "/veil.db"
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	created, err := repo.Create(Client{Name: "token-race", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	store := NewTokenStore(db)
	issued, err := store.Issue(created.ID, "race", nil)
	if err != nil {
		t.Fatal(err)
	}

	revokerDB, err := storage.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer revokerDB.Close()
	tx, err := revokerDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := time.Now().Unix()
	if _, err := tx.Exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=?`, revokedAt, issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	type lookupResult struct {
		token *SubscriptionToken
		err   error
	}
	done := make(chan lookupResult, 1)
	go func() {
		token, err := store.LookupByPlaintext(issued.Plaintext)
		done <- lookupResult{token: token, err: err}
	}()
	// The read-only lookup may linearize before the uncommitted revoke, but its
	// asynchronous telemetry must never update a token after the revoke wins.
	time.Sleep(150 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("lookup surfaced a transient race error: %v", result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lookup deadlocked with concurrent revoke")
	}
	if token, err := store.LookupReadOnly(issued.Plaintext); err != nil || token != nil {
		t.Fatalf("post-commit lookup = %+v, %v; want revoked/not-found", token, err)
	}
	time.Sleep(150 * time.Millisecond)
	var lastUsed *int64
	if err := db.QueryRow(`SELECT last_used_at FROM subscription_tokens WHERE id=?`, issued.Token.ID).Scan(&lastUsed); err != nil {
		t.Fatal(err)
	}
	if lastUsed != nil {
		t.Fatalf("last_used_at updated after revoke won: %d", *lastUsed)
	}
}
