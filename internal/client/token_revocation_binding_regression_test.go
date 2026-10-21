package client

import (
	"testing"
)

// #1346: revoked_at is a plain veil.db column. Revocation (and the revoke half
// of rotation) must destroy the sealed binding, so a veil.db writer cannot
// un-revoke a leaked or rotated-out token with `revoked_at=NULL`.

func storedTokenCiphertext(t *testing.T, f *bindingFixture, id string) string {
	t.Helper()
	var ciphertext string
	if err := f.ts.db.QueryRow(`SELECT token_ciphertext FROM subscription_tokens WHERE id=?`, id).Scan(&ciphertext); err != nil {
		t.Fatalf("read token_ciphertext: %v", err)
	}
	return ciphertext
}

func TestRevokedTokenCannotBeResurrectedByClearingRevokedAt(t *testing.T) {
	f, exec := newBindingFixture(t)
	leaked, err := f.ts.Issue(f.alice.ID, "leaked", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.ts.LookupReadOnly(leaked.Plaintext); err != nil || got == nil {
		t.Fatalf("issued token must authenticate before revocation: %v %v", got, err)
	}
	if err := f.ts.Revoke(leaked.Token.ID); err != nil {
		t.Fatal(err)
	}
	if c := storedTokenCiphertext(t, f, leaked.Token.ID); c != "" {
		t.Fatalf("Revoke must destroy the sealed binding, token_ciphertext=%q", c)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, leaked.Token.ID)
	assertTokenRefused(t, f.ts, leaked.Plaintext)
	if _, err := f.ts.Reveal(leaked.Token.ID); err == nil {
		t.Fatal("Reveal returned a secret for a revoked-then-un-revoked token")
	}
}

func TestRotatedOutTokenCannotBeResurrectedByClearingRevokedAt(t *testing.T) {
	f, exec := newBindingFixture(t)
	old, err := f.ts.Issue(f.bob.ID, "old", nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.ts.Rotate(old.Token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c := storedTokenCiphertext(t, f, old.Token.ID); c != "" {
		t.Fatalf("Rotate must destroy the old row's sealed binding, token_ciphertext=%q", c)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, old.Token.ID)
	assertTokenRefused(t, f.ts, old.Plaintext)
	if got, err := f.ts.LookupReadOnly(fresh.Plaintext); err != nil || got == nil || got.ID != fresh.Token.ID {
		t.Fatalf("replacement token must still authenticate: %+v %v", got, err)
	}
}

func TestRevokeTokenTxDestroysBinding(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "tx", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.ts.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx := &Tx{queries: queries{q: raw}, tx: raw}
	if err := tx.RevokeTokenTx(issued.Token.ID); err != nil {
		_ = raw.Rollback()
		t.Fatal(err)
	}
	if err := raw.Commit(); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, issued.Token.ID)
	assertTokenRefused(t, f.ts, issued.Plaintext)
}

// A row revoked by a build before #1346 still carries its valid binding. The
// startup pass must destroy it, and must leave live tokens alone.
func TestSealLegacyBindingsScrubsBindingsOfPreviouslyRevokedRows(t *testing.T) {
	f, exec := newBindingFixture(t)
	revoked, err := f.ts.Issue(f.alice.ID, "revoked-pre-1346", nil)
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.ts.Issue(f.bob.ID, "live", nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed := storedTokenCiphertext(t, f, revoked.Token.ID)
	// Model the old revoke: revoked_at set, binding left in place.
	exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=?`, nowUnix(), revoked.Token.ID)
	if storedTokenCiphertext(t, f, revoked.Token.ID) != sealed {
		t.Fatal("fixture: legacy revoke must keep the binding")
	}

	restarted := NewTokenStore(f.ts.db).WithCipher(f.ts.cipher)
	t.Cleanup(restarted.Close)
	if _, err := restarted.SealLegacyBindings(); err != nil {
		t.Fatal(err)
	}
	if c := storedTokenCiphertext(t, f, revoked.Token.ID); c != "" {
		t.Fatalf("startup must clear a revoked row's binding, token_ciphertext=%q", c)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, revoked.Token.ID)
	assertTokenRefused(t, restarted, revoked.Plaintext)
	if got, err := restarted.LookupReadOnly(live.Plaintext); err != nil || got == nil || got.ID != live.Token.ID {
		t.Fatalf("live token must survive the scrub: %+v %v", got, err)
	}
	if _, err := restarted.Reveal(live.Token.ID); err != nil {
		t.Fatalf("live token must stay revealable: %v", err)
	}
}

// The re-activated row is now a live row without a valid seal; if the
// migration marker is also deleted, the legacy pass must still fail closed.
func TestUnRevokedScrubbedRowBlocksLegacyPassReArm(t *testing.T) {
	f, exec := newBindingFixture(t)
	issued, err := f.ts.Issue(f.alice.ID, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ts.Revoke(issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, issued.Token.ID)
	exec(`DELETE FROM migration_markers WHERE key=?`, TokenBindingMarkerKey)
	plantLegacyToken(t, f, exec, "planted-legacy", f.bob.ID, "attacker-chosen-legacy")
	restarted := NewTokenStore(f.ts.db).WithCipher(f.ts.cipher)
	t.Cleanup(restarted.Close)
	if n, err := restarted.SealLegacyBindings(); err != nil || n != 0 {
		t.Fatalf("SealLegacyBindings = %d, %v; want 0, nil", n, err)
	}
	assertTokenRefused(t, restarted, issued.Plaintext)
	assertTokenRefused(t, restarted, "attacker-chosen-legacy")
}

// #1348 review: when the startup pass did not complete, the first Issue or
// Rotate finishes it through ensureLegacyBindingsSealed; that path must clear
// revoked rows' bindings too.
func TestFirstIssueAfterFailedStartupScrubsRevokedBindings(t *testing.T) {
	f, exec := newBindingFixture(t)
	revoked, err := f.ts.Issue(f.alice.ID, "revoked-pre-1346", nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=?`, nowUnix(), revoked.Token.ID)

	// A restarted store whose startup SealLegacyBindings never succeeded.
	restarted := NewTokenStore(f.ts.db).WithCipher(f.ts.cipher)
	t.Cleanup(restarted.Close)
	if _, err := restarted.Issue(f.bob.ID, "first-after-restart", nil); err != nil {
		t.Fatal(err)
	}
	if c := storedTokenCiphertext(t, f, revoked.Token.ID); c != "" {
		t.Fatalf("first Issue must complete the revoked-binding clear, token_ciphertext=%q", c)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=NULL WHERE id=?`, revoked.Token.ID)
	assertTokenRefused(t, restarted, revoked.Plaintext)
}

// A failed clear must not mark the pass complete: the next caller retries it.
func TestFailedRevokedBindingClearIsRetried(t *testing.T) {
	f, exec := newBindingFixture(t)
	revoked, err := f.ts.Issue(f.alice.ID, "revoked-pre-1346", nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=?`, nowUnix(), revoked.Token.ID)
	exec(`CREATE TRIGGER fail_scrub BEFORE UPDATE OF token_ciphertext ON subscription_tokens
	      WHEN NEW.token_ciphertext='' AND OLD.revoked_at IS NOT NULL
	      BEGIN SELECT RAISE(ABORT,'injected scrub failure'); END`)
	restarted := NewTokenStore(f.ts.db).WithCipher(f.ts.cipher)
	t.Cleanup(restarted.Close)
	if _, err := restarted.SealLegacyBindings(); err == nil {
		t.Fatal("SealLegacyBindings must report a failed clear")
	}
	exec(`DROP TRIGGER fail_scrub`)
	if _, err := restarted.Issue(f.bob.ID, "retry", nil); err != nil {
		t.Fatal(err)
	}
	if c := storedTokenCiphertext(t, f, revoked.Token.ID); c != "" {
		t.Fatalf("a failed clear must be retried by the next Issue, token_ciphertext=%q", c)
	}
}
