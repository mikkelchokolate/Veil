package client

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"
)

// #1281: a subscription_tokens row must not be self-authorizing. A veil.db
// writer without the state key must not be able to plant, re-point or extend
// a token row and turn /s/{token} into an unauthenticated decrypt-as-a-service.

type bindingFixture struct {
	ts    *TokenStore
	alice Client
	bob   Client
}

func newBindingFixture(t *testing.T) (*bindingFixture, func(string, ...any)) {
	t.Helper()
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	repo := NewRepository(db)
	ts := NewTokenStore(db).WithCipher(newTestCipher(t))
	t.Cleanup(ts.Close)
	alice, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	return &bindingFixture{ts: ts, alice: alice, bob: bob}, exec
}

func assertTokenRefused(t *testing.T, ts *TokenStore, plaintext string) {
	t.Helper()
	for name, lookup := range map[string]func(string) (*SubscriptionToken, error){
		"LookupReadOnly":    ts.LookupReadOnly,
		"LookupByPlaintext": ts.LookupByPlaintext,
	} {
		got, err := lookup(plaintext)
		if err != nil {
			t.Fatalf("%s returned an error (want an indistinguishable miss): %v", name, err)
		}
		if got != nil {
			t.Fatalf("%s authenticated a row that was not issued by the panel: %+v", name, got)
		}
	}
}

func sha(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}

func TestPlantedTokenRowDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label)
	      VALUES ('planted', ?, ?, 'atk', 1, ?, '', '')`, f.alice.ID, sha("atk-chosen"), time.Now().Unix())
	assertTokenRefused(t, f.ts, "atk-chosen")
}

func TestPlantedTokenRowWithGarbageOrCopiedCiphertextDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	real, err := f.ts.Issue(f.bob.ID, "real", nil)
	if err != nil {
		t.Fatal(err)
	}
	var copied string
	if err := f.ts.db.QueryRow(`SELECT token_ciphertext FROM subscription_tokens WHERE id=?`, real.Token.ID).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	for i, ciphertext := range []string{"atk-chosen", "ve1:garbage", copied} {
		id := "planted-" + string(rune('a'+i))
		exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
		      VALUES (?, ?, ?, 'atk', 1, ?, '', '', ?)`, id, f.alice.ID, sha("atk-chosen-"+id), time.Now().Unix(), ciphertext)
		assertTokenRefused(t, f.ts, "atk-chosen-"+id)
	}
	// Cipher.Decrypt passes non-"ve1:" input through verbatim (#1283): a
	// row whose "ciphertext" is the chosen token itself must still be refused.
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
	      VALUES ('planted-passthrough', ?, ?, 'atk', 1, ?, '', '', 'atk-passthrough')`, f.alice.ID, sha("atk-passthrough"), time.Now().Unix())
	assertTokenRefused(t, f.ts, "atk-passthrough")
	// The honest row is untouched and still works.
	if got, err := f.ts.LookupReadOnly(real.Plaintext); err != nil || got == nil || got.ClientID != f.bob.ID {
		t.Fatalf("honest token lookup = %+v, %v", got, err)
	}
}

func TestRewrittenTokenHashDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET token_hash=? WHERE id=?`, sha("atk-chosen"), issued.Token.ID)
	assertTokenRefused(t, f.ts, "atk-chosen")
}

func TestTokenRowRepointedToAnotherClientDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	// The attacker legitimately holds alice's token and re-points its row at bob.
	issued, err := f.ts.Issue(f.alice.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET client_id=? WHERE id=?`, f.bob.ID, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
	if got, err := f.ts.Reveal(issued.Token.ID); err == nil {
		t.Fatalf("Reveal of a re-pointed row = %q, want error", got)
	}
}

func TestTokenRowRenamedDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A renamed row would dodge a revoke the operator issues by the old id.
	exec(`UPDATE subscription_tokens SET id='renamed' WHERE id=?`, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
}

func TestTokenRowCiphertextSwappedBetweenRowsDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	aliceTok, err := f.ts.Issue(f.alice.ID, "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	bobTok, err := f.ts.Issue(f.bob.ID, "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Move alice's (hash, ciphertext) pair onto bob's row id/client.
	var hash []byte
	var ciphertext string
	if err := f.ts.db.QueryRow(`SELECT token_hash, token_ciphertext FROM subscription_tokens WHERE id=?`, aliceTok.Token.ID).Scan(&hash, &ciphertext); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM subscription_tokens WHERE id=?`, aliceTok.Token.ID)
	exec(`UPDATE subscription_tokens SET token_hash=?, token_ciphertext=? WHERE id=?`, hash, ciphertext, bobTok.Token.ID)
	assertTokenRefused(t, f.ts, aliceTok.Plaintext)
}

func TestTokenRowExpiryExtensionDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	expiry := time.Now().Add(time.Hour).Unix()
	issued, err := f.ts.Issue(f.alice.ID, "phone", &expiry)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.ts.LookupReadOnly(issued.Plaintext); err != nil || got == nil {
		t.Fatalf("honest expiring token lookup = %+v, %v", got, err)
	}
	exec(`UPDATE subscription_tokens SET expires_at=NULL WHERE id=?`, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
	exec(`UPDATE subscription_tokens SET expires_at=? WHERE id=?`, expiry+86400, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
}

func TestIssuedAndRotatedTokensAuthenticateAndReveal(t *testing.T) {
	f, _ := newBindingFixture(t)
	expiry := time.Now().Add(time.Hour).Unix()
	issued, err := f.ts.IssueBy(f.alice.ID, "phone", "admin", &expiry)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.ts.LookupByPlaintext(issued.Plaintext)
	if err != nil || got == nil || got.ClientID != f.alice.ID || got.ID != issued.Token.ID {
		t.Fatalf("issued lookup = %+v, %v", got, err)
	}
	if revealed, err := f.ts.Reveal(issued.Token.ID); err != nil || revealed != issued.Plaintext {
		t.Fatalf("issued reveal = %q, %v", revealed, err)
	}
	newExpiry := expiry + 3600
	rotated, err := f.ts.RotateWithExpiry(issued.Token.ID, &newExpiry, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.ts.LookupReadOnly(rotated.Plaintext); err != nil || got == nil || got.ID != rotated.Token.ID {
		t.Fatalf("rotated lookup = %+v, %v", got, err)
	}
	if revealed, err := f.ts.Reveal(rotated.Token.ID); err != nil || revealed != rotated.Plaintext {
		t.Fatalf("rotated reveal = %q, %v", revealed, err)
	}
	assertTokenRefused(t, f.ts, issued.Plaintext)
}

// Rows issued before #1281 sealed the bare plaintext. They stay valid: the
// AEAD still proves the panel wrote that (hash, ciphertext) pair.
func TestLegacyBareCiphertextTokenStillAuthenticates(t *testing.T) {
	f, exec := newBindingFixture(t)
	plaintext := "legacy-token-plaintext"
	sealed, err := f.ts.cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
	      VALUES ('legacy', ?, ?, 'legacy-t', 1, ?, '', '', ?)`, f.alice.ID, sha(plaintext), time.Now().Unix(), sealed)
	if got, err := f.ts.LookupReadOnly(plaintext); err != nil || got == nil || got.ClientID != f.alice.ID {
		t.Fatalf("legacy lookup = %+v, %v", got, err)
	}
	if revealed, err := f.ts.Reveal("legacy"); err != nil || revealed != plaintext {
		t.Fatalf("legacy reveal = %q, %v", revealed, err)
	}
	// A legacy ciphertext whose plaintext does not hash to the row is refused.
	exec(`UPDATE subscription_tokens SET token_hash=? WHERE id='legacy'`, sha("atk-chosen"))
	assertTokenRefused(t, f.ts, "atk-chosen")
	if got, err := f.ts.Reveal("legacy"); err == nil {
		t.Fatalf("Reveal of a legacy row with a rewritten hash = %q, want error", got)
	}
}

func TestTokenStoreWithoutCipherFailsClosed(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	alice, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	withCipher := NewTokenStore(db).WithCipher(newTestCipher(t))
	defer withCipher.Close()
	issued, err := withCipher.Issue(alice.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bare := NewTokenStore(db)
	defer bare.Close()
	if _, err := bare.Issue(alice.ID, "", nil); err == nil {
		t.Fatal("Issue without a cipher must fail: the row could never be verified")
	}
	assertTokenRefused(t, bare, issued.Plaintext)
}

func TestTokenBindingWithUnknownVersionDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tokenBinding{Version: "veil-subscription-token-binding-v0", ID: issued.Token.ID, ClientID: f.alice.ID, Token: issued.Plaintext})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.ts.cipher.Encrypt(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, sealed, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
}
