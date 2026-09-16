package statecommit

import (
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// Issue #162: key rotation must re-encrypt every live AES-GCM blob in the same
// transaction as client_credentials — subscription token reveal ciphertext and
// durable idempotency replay envelopes included.
func TestRotateKeyReencryptsSubscriptionTokenCiphertext(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repository := client.NewRepository(db)
	clientRow, err := repository.Create(client.Client{Name: "alice", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := client.NewTokenStore(db).WithCipher(oldCipher)
	issued, err := tokenStore.Issue(clientRow.ID, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	tokenStore.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}

	newKey, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := cipherFromBytes(t, newKey)
	db, err = storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := client.NewTokenStore(db).WithCipher(newCipher)
	t.Cleanup(store.Close)
	revealed, err := store.Reveal(issued.Token.ID)
	if err != nil {
		t.Fatalf("reveal with rotated key: %v", err)
	}
	if revealed != issued.Plaintext {
		t.Fatal("revealed token plaintext changed across rotation")
	}

	stale := client.NewTokenStore(db).WithCipher(oldCipher)
	t.Cleanup(stale.Close)
	if _, err := stale.Reveal(issued.Token.ID); err == nil {
		t.Fatal("token ciphertext still decrypts with the retired key")
	}
}

func TestRotateKeyReencryptsIdempotencyReplayEnvelopes(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	oldReplay, err := secrets.DeriveCipher(oldCipher, secrets.IdempotencyReplayLabel)
	if err != nil {
		t.Fatal(err)
	}
	envelopeJSON := `{"actor":"alice","body":"c2VjcmV0"}`
	sealed, err := oldReplay.Encrypt(envelopeJSON)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency_results
	  (id,scope,operation_generation,response_status,response_headers,response_body,encrypted,created_at,expires_at)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		"result-1", "scope-1", 1, 200, []byte(`{}`), []byte(sealed), 1, 1, 9_999_999_999); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}

	newKey, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := cipherFromBytes(t, newKey)
	newReplay, err := secrets.DeriveCipher(newCipher, secrets.IdempotencyReplayLabel)
	if err != nil {
		t.Fatal(err)
	}
	db, err = storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body []byte
	if err := db.QueryRow(`SELECT response_body FROM idempotency_results WHERE id='result-1'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	plaintext, err := newReplay.Decrypt(string(body))
	if err != nil {
		t.Fatalf("replay envelope does not decrypt with the rotated derived cipher: %v", err)
	}
	if plaintext != envelopeJSON {
		t.Fatalf("replay envelope changed across rotation: %q", plaintext)
	}
	if _, err := oldReplay.Decrypt(string(body)); err == nil {
		t.Fatal("replay envelope still decrypts with the retired derived cipher")
	}
}
