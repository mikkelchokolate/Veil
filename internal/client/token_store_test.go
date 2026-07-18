package client

import (
	"testing"
)

func TestTokenIssuedHashedOnlyPlaintextShownOnce(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	issued, err := ts.Issue(c.ID, "phone", nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Plaintext == "" {
		t.Fatalf("expected plaintext returned once at issue")
	}
	if issued.Token.Prefix == "" {
		t.Fatalf("expected prefix for identification")
	}
	// The stored hash must not equal the plaintext.
	if issued.Token.TokenHash == issued.Plaintext {
		t.Fatalf("token stored in plaintext")
	}
	// A lookup by plaintext must resolve the token via hashing.
	got, err := ts.LookupByPlaintext(issued.Plaintext)
	if err != nil || got == nil {
		t.Fatalf("lookup by plaintext failed: %v", err)
	}
	if got.ClientID != c.ID {
		t.Fatalf("wrong client: %v", got.ClientID)
	}
	// Re-issuing reveals nothing new: a second call to Issue returns a NEW
	// token with a NEW plaintext; the original plaintext is not re-derivable.
	second, _ := ts.Issue(c.ID, "laptop", nil)
	if second.Plaintext == issued.Plaintext {
		t.Fatalf("tokens must be unique per issue")
	}
}

func TestTokenLookupUpdatesLastUsed(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db)
	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	issued, _ := ts.Issue(c.ID, "", nil)
	got, err := ts.LookupByPlaintext(issued.Plaintext)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Fatalf("expected lastUsedAt recorded")
	}
}

func TestRevokedTokenNoLongerWorks(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db)
	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	issued, _ := ts.Issue(c.ID, "", nil)
	if err := ts.Revoke(issued.Token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, err := ts.LookupByPlaintext(issued.Plaintext)
	if err == nil && got != nil {
		t.Fatalf("revoked token must not resolve")
	}
}

func TestRotateIssuesNewInvalidatesOld(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db)
	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	issued, _ := ts.Issue(c.ID, "", nil)
	rotated, err := ts.Rotate(issued.Token.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Plaintext == "" {
		t.Fatalf("rotate must return the new plaintext once")
	}
	// Old plaintext must be dead.
	if got, _ := ts.LookupByPlaintext(issued.Plaintext); got != nil {
		t.Fatalf("old token must be invalid after rotate")
	}
	// New plaintext works.
	if got, _ := ts.LookupByPlaintext(rotated.Plaintext); got == nil {
		t.Fatalf("new token must work after rotate")
	}
}

func TestListTokensOmitsHashAndPlaintext(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db)
	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	_, _ = ts.Issue(c.ID, "phone", nil)
	tokens, err := ts.ListForClient(c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token")
	}
	if tokens[0].TokenHash != "" {
		t.Fatalf("list must not expose token hash")
	}
}
