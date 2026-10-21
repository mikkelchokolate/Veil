package client

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/secrets"
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

// plantLegacyToken inserts a row the way the panel issued tokens before
// #1281: the bare token sealed under the master cipher.
func plantLegacyToken(t *testing.T, f *bindingFixture, exec func(string, ...any), id, clientID, plaintext string) {
	t.Helper()
	sealed, err := f.ts.cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
	      VALUES (?, ?, ?, 'legacy-t', 1, ?, '', '', ?)`, id, clientID, sha(plaintext), time.Now().Unix(), sealed)
}

// #1281 review: a client's own credential is sealed under the same master
// cipher as a pre-#1281 token. Copying it into a planted row must not turn
// "/s/<my own password>" into another client's subscription.
func TestCredentialCiphertextPlantedAsTokenDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	repo := NewRepository(f.ts.db)
	binding, err := repo.CreateBinding(Binding{ClientID: f.bob.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Production runs the one-shot re-seal when it wires the store.
	if _, err := f.ts.SealLegacyBindings(); err != nil {
		t.Fatal(err)
	}
	const bobPassword = "bob-own-hysteria2-password"
	cred, err := NewCredentialStore(f.ts.db, f.ts.cipher).Set(binding.ID, "password", bobPassword)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
	      VALUES ('planted-cred', ?, ?, 'x', 1, ?, '', '', (SELECT encrypted_value FROM client_credentials WHERE id=?))`,
		f.alice.ID, sha(bobPassword), time.Now().Unix(), cred.ID)
	assertTokenRefused(t, f.ts, bobPassword)
	if got, err := f.ts.Reveal("planted-cred"); err == nil {
		t.Fatalf("Reveal of a planted credential ciphertext = %q, want error", got)
	}
	// The one-shot re-seal has already run on this database, so running it
	// again (a panel restart) must not launder the planted row either.
	if _, err := f.ts.SealLegacyBindings(); err != nil {
		t.Fatal(err)
	}
	assertTokenRefused(t, f.ts, bobPassword)
}

func TestSealLegacyBindingsResealsPreBindingTokensOnce(t *testing.T) {
	f, exec := newBindingFixture(t)
	const plaintext = "legacy-token-plaintext"
	plantLegacyToken(t, f, exec, "legacy", f.alice.ID, plaintext)
	// A bare master-cipher seal never authenticates by itself.
	assertTokenRefused(t, f.ts, plaintext)

	resealed, err := f.ts.SealLegacyBindings()
	if err != nil || resealed != 1 {
		t.Fatalf("SealLegacyBindings = %d, %v; want 1", resealed, err)
	}
	if got, err := f.ts.LookupReadOnly(plaintext); err != nil || got == nil || got.ClientID != f.alice.ID {
		t.Fatalf("re-sealed legacy lookup = %+v, %v", got, err)
	}
	if revealed, err := f.ts.Reveal("legacy"); err != nil || revealed != plaintext {
		t.Fatalf("re-sealed legacy reveal = %q, %v", revealed, err)
	}
	listed, err := f.ts.ListForClient(f.alice.ID)
	if err != nil || len(listed) != 1 || !listed[0].HasSecret {
		t.Fatalf("re-sealed legacy list = %+v, %v", listed, err)
	}
	// Once re-sealed, the row is bound to its client like a new token.
	exec(`UPDATE subscription_tokens SET client_id=? WHERE id='legacy'`, f.bob.ID)
	assertTokenRefused(t, f.ts, plaintext)
	exec(`UPDATE subscription_tokens SET client_id=? WHERE id='legacy'`, f.alice.ID)

	// The pass is one-shot: a bare seal planted after it stays refused.
	plantLegacyToken(t, f, exec, "late", f.alice.ID, "late-legacy-plaintext")
	if resealed, err := f.ts.SealLegacyBindings(); err != nil || resealed != 0 {
		t.Fatalf("second SealLegacyBindings = %d, %v; want 0", resealed, err)
	}
	assertTokenRefused(t, f.ts, "late-legacy-plaintext")
	if _, err := f.ts.Reveal("late"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Reveal of a late bare seal = %v, want ErrSecretUnavailable", err)
	}
	listed, err = f.ts.ListForClient(f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range listed {
		if want := token.ID == "legacy"; token.HasSecret != want {
			t.Fatalf("HasSecret(%s) = %v, want %v", token.ID, token.HasSecret, want)
		}
	}
}

func TestSealLegacyBindingsRefusesWhenMarkerWasDeleted(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ts.SealLegacyBindings(); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM migration_markers WHERE key=?`, TokenBindingMarkerKey)
	plantLegacyToken(t, f, exec, "planted", f.alice.ID, "planted-after-upgrade")
	// A database that already holds a sealed binding has been through the
	// pass; a missing marker means tampering, so nothing is re-sealed.
	if resealed, err := f.ts.SealLegacyBindings(); err != nil || resealed != 0 {
		t.Fatalf("SealLegacyBindings after marker deletion = %d, %v; want 0", resealed, err)
	}
	assertTokenRefused(t, f.ts, "planted-after-upgrade")
	if got, err := f.ts.LookupReadOnly(issued.Plaintext); err != nil || got == nil {
		t.Fatalf("honest token lookup = %+v, %v", got, err)
	}
	var count int
	if err := f.ts.db.QueryRow(`SELECT COUNT(*) FROM migration_markers WHERE key=?`, TokenBindingMarkerKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("marker not restored: count=%d err=%v", count, err)
	}
}

func TestReencryptSubscriptionTokensCarriesBindingsAndLegacySeals(t *testing.T) {
	f, exec := newBindingFixture(t)
	// A pre-#1281 database rotated offline before the upgraded panel ever
	// started: only bare master-cipher seals exist.
	plantLegacyToken(t, f, exec, "legacy", f.bob.ID, "legacy-token-plaintext")
	newCipher := rotationTestCipher(t, 200)
	reencryptTokens(t, f, f.ts.cipher, newCipher)
	rotated := NewTokenStore(f.ts.db).WithCipher(newCipher)
	t.Cleanup(rotated.Close)
	if resealed, err := rotated.SealLegacyBindings(); err != nil || resealed != 1 {
		t.Fatalf("SealLegacyBindings after rotation = %d, %v; want 1", resealed, err)
	}
	if got, err := rotated.LookupReadOnly("legacy-token-plaintext"); err != nil || got == nil || got.ClientID != f.bob.ID {
		t.Fatalf("legacy lookup after rotation = %+v, %v", got, err)
	}
	// Bindings move to the next key as bindings.
	issued, err := rotated.Issue(f.alice.ID, "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	nextCipher := rotationTestCipher(t, 100)
	reencryptTokens(t, f, newCipher, nextCipher)
	next := NewTokenStore(f.ts.db).WithCipher(nextCipher)
	t.Cleanup(next.Close)
	for _, plaintext := range []string{issued.Plaintext, "legacy-token-plaintext"} {
		if got, err := next.LookupReadOnly(plaintext); err != nil || got == nil {
			t.Fatalf("binding lookup after second rotation = %+v, %v", got, err)
		}
		assertTokenRefused(t, rotated, plaintext)
	}
}

func rotationTestCipher(t *testing.T, seed byte) *secrets.Cipher {
	t.Helper()
	var key [secrets.KeySize]byte
	for i := range key {
		key[i] = seed - byte(i)
	}
	c, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func reencryptTokens(t *testing.T, f *bindingFixture, from, to *secrets.Cipher) {
	t.Helper()
	tx, err := NewRepository(f.ts.db).BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.ReencryptSubscriptionTokens(from, to); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueResealsLegacyTokensFirst(t *testing.T) {
	f, exec := newBindingFixture(t)
	// The startup pass failed or has not run yet; the first issuance must
	// not strand pre-#1281 tokens by creating a binding ahead of it.
	plantLegacyToken(t, f, exec, "legacy", f.alice.ID, "legacy-token-plaintext")
	if _, err := f.ts.Issue(f.bob.ID, "new", nil); err != nil {
		t.Fatal(err)
	}
	if got, err := f.ts.LookupReadOnly("legacy-token-plaintext"); err != nil || got == nil || got.ClientID != f.alice.ID {
		t.Fatalf("legacy lookup after first issue = %+v, %v", got, err)
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
	if _, err := bare.SealLegacyBindings(); err == nil {
		t.Fatal("SealLegacyBindings without a cipher must fail")
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
	sealed, err := f.ts.binding.Encrypt(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, sealed, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
}

// A well-formed binding sealed under the master key instead of the token
// subkey is refused: only the subkey proves a token seal.
func TestTokenBindingSealedUnderMasterKeyDoesNotAuthenticate(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tokenBinding{Version: tokenBindingVersion, ID: issued.Token.ID, ClientID: f.alice.ID, Token: issued.Plaintext})
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

// freshTokenStore models a panel restart: the in-memory legacySealed flag is
// gone and only veil.db (plus the state key) carries over.
func freshTokenStore(t *testing.T, f *bindingFixture) *TokenStore {
	t.Helper()
	ts := NewTokenStore(f.ts.db).WithCipher(f.ts.cipher)
	t.Cleanup(ts.Close)
	return ts
}

// plantCredentialCopy copies bob's real credential ciphertext into a row for
// alice whose token_hash is sha256(bob's password) (#1281 review).
func plantCredentialCopy(t *testing.T, f *bindingFixture, exec func(string, ...any), id, password string) {
	t.Helper()
	binding, err := NewRepository(f.ts.db).CreateBinding(Binding{ClientID: f.bob.ID, InboundID: "hy-" + id, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := NewCredentialStore(f.ts.db, f.ts.cipher).Set(binding.ID, "password", password)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, created_by, label, token_ciphertext)
	      VALUES (?, ?, ?, 'x', 1, ?, '', '', (SELECT encrypted_value FROM client_credentials WHERE id=?))`,
		id, f.alice.ID, sha(password), time.Now().Unix(), cred.ID)
}

// #1281 review (f936a087): deleting the marker and blanking every honest
// binding must not re-arm the one-shot re-seal on the next restart.
func TestSealLegacyBindingsNotReArmedByMarkerDeletionAndBlankedRows(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "honest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ts.SealLegacyBindings(); err != nil {
		t.Fatal(err)
	}
	const bobPassword = "bob-own-hysteria2-password"
	exec(`DELETE FROM migration_markers WHERE key=?`, TokenBindingMarkerKey)
	exec(`UPDATE subscription_tokens SET token_ciphertext='' WHERE id=?`, issued.Token.ID)
	plantLegacyToken(t, f, exec, "planted-bare", f.alice.ID, "planted-after-upgrade")
	plantCredentialCopy(t, f, exec, "planted-cred", bobPassword)

	restarted := freshTokenStore(t, f)
	if resealed, err := restarted.SealLegacyBindings(); err != nil || resealed != 0 {
		t.Fatalf("SealLegacyBindings after marker deletion + blanking = %d, %v; want 0", resealed, err)
	}
	assertTokenRefused(t, restarted, bobPassword)
	assertTokenRefused(t, restarted, "planted-after-upgrade")
	var count int
	if err := f.ts.db.QueryRow(`SELECT COUNT(*) FROM migration_markers WHERE key=?`, TokenBindingMarkerKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("marker not restored: count=%d err=%v", count, err)
	}
}

// #1281 review (f936a087): on an install that never issued a token there is
// no honest row to blank, so the credential-copy primitive must be refused
// by the re-seal itself, both as a byte copy and as a fresh seal of the same
// credential plaintext.
func TestSealLegacyBindingsNeverResealsCredentialSeals(t *testing.T) {
	f, exec := newBindingFixture(t)
	const bobPassword = "bob-own-hysteria2-password"
	plantCredentialCopy(t, f, exec, "planted-cred", bobPassword)
	const otherPassword = "bob-other-credential"
	if _, err := NewCredentialStore(f.ts.db, f.ts.cipher).Set(mustBinding(t, f, "hy-other"), "password", otherPassword); err != nil {
		t.Fatal(err)
	}
	plantLegacyToken(t, f, exec, "planted-reseal", f.alice.ID, otherPassword)

	restarted := freshTokenStore(t, f)
	if resealed, err := restarted.SealLegacyBindings(); err != nil || resealed != 0 {
		t.Fatalf("SealLegacyBindings with credential seals = %d, %v; want 0", resealed, err)
	}
	assertTokenRefused(t, restarted, bobPassword)
	assertTokenRefused(t, restarted, otherPassword)
	if _, err := restarted.Reveal("planted-cred"); err == nil {
		t.Fatal("Reveal of a planted credential ciphertext succeeded")
	}
}

func mustBinding(t *testing.T, f *bindingFixture, inbound string) string {
	t.Helper()
	binding, err := NewRepository(f.ts.db).CreateBinding(Binding{ClientID: f.bob.ID, InboundID: inbound, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return binding.ID
}

// A revoked row never authenticates, so a revoked row with a cleared
// ciphertext (the operator recovery the master-key rotation error suggests)
// must not strand the pre-#1281 tokens of an honest upgrade.
func TestSealLegacyBindingsIgnoresRevokedBlankRows(t *testing.T) {
	f, exec := newBindingFixture(t)
	plantLegacyToken(t, f, exec, "legacy", f.alice.ID, "legacy-token-plaintext")
	exec(`INSERT INTO subscription_tokens (id, client_id, token_hash, token_prefix, enabled, created_at, revoked_at, created_by, label, token_ciphertext)
	      VALUES ('revoked-blank', ?, ?, 'r', 0, ?, ?, '', '', '')`, f.bob.ID, sha("revoked"), time.Now().Unix(), time.Now().Unix())
	if resealed, err := f.ts.SealLegacyBindings(); err != nil || resealed != 1 {
		t.Fatalf("SealLegacyBindings = %d, %v; want 1", resealed, err)
	}
	if got, err := f.ts.LookupReadOnly("legacy-token-plaintext"); err != nil || got == nil || got.ClientID != f.alice.ID {
		t.Fatalf("legacy lookup = %+v, %v", got, err)
	}
}

// #1281 review (f936a087): after the one-shot pass, master-key rotation must
// not carry a bare seal (a planted credential copy) forward to the new key.
func TestReencryptSubscriptionTokensDropsBareSealsAfterMarker(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "honest", nil)
	if err != nil {
		t.Fatal(err)
	}
	const bobPassword = "bob-own-hysteria2-password"
	plantCredentialCopy(t, f, exec, "planted-cred", bobPassword)
	newCipher := rotationTestCipher(t, 200)
	reencryptTokens(t, f, f.ts.cipher, newCipher)
	var stale string
	if err := f.ts.db.QueryRow(`SELECT token_ciphertext FROM subscription_tokens WHERE id='planted-cred'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != "" {
		t.Fatalf("bare seal carried across master-key rotation: %q", stale)
	}
	rotated := NewTokenStore(f.ts.db).WithCipher(newCipher)
	t.Cleanup(rotated.Close)
	if got, err := rotated.LookupReadOnly(issued.Plaintext); err != nil || got == nil {
		t.Fatalf("honest token after rotation = %+v, %v", got, err)
	}
	assertTokenRefused(t, rotated, bobPassword)
}
