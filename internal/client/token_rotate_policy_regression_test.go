package client

import (
	"testing"
	"time"
)

// #1272 item 2/4: rotation is "same policy, new secret". The replacement row
// keeps the label, the issuer (created_by) and the enabled flag of the row it
// replaces. Before the fix, issueTokenTx hardcoded created_by="" and
// enabled=true, so a rotation erased who issued the token and silently
// re-enabled a disabled one.
func TestRotateKeepsLabelIssuerAndExpiry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db).WithCipher(newTestCipher(t))
	c, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	expiry := time.Now().Add(48 * time.Hour).Unix()
	issued, err := ts.IssueBy(c.ID, "phone", "operator-1", &expiry)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rotated, err := ts.RotateWithExpiry(issued.Token.ID, nil, false)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Token.CreatedBy != "operator-1" {
		t.Fatalf("returned createdBy = %q, want operator-1", rotated.Token.CreatedBy)
	}
	stored, err := ts.Get(rotated.Token.ID)
	if err != nil {
		t.Fatalf("get rotated: %v", err)
	}
	if stored.CreatedBy != "operator-1" || stored.Label != "phone" || !stored.Enabled {
		t.Fatalf("rotated row = createdBy %q label %q enabled %v, want operator-1/phone/true",
			stored.CreatedBy, stored.Label, stored.Enabled)
	}
	if stored.ExpiresAt == nil || *stored.ExpiresAt != expiry {
		t.Fatalf("rotated expiry = %v, want %d (omitted expiresAt keeps the current one)", stored.ExpiresAt, expiry)
	}
	if got, err := ts.LookupReadOnly(rotated.Plaintext); err != nil || got == nil {
		t.Fatalf("rotated token does not authenticate: %v", err)
	}
}

func TestRotateKeepsDisabledTokenDisabled(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db).WithCipher(newTestCipher(t))
	c, err := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	issued, err := ts.Issue(c.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := ts.SetEnabled(issued.Token.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	rotated, err := ts.RotateWithExpiry(issued.Token.ID, nil, false)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Token.Enabled {
		t.Fatalf("rotation re-enabled a disabled token")
	}
	stored, err := ts.Get(rotated.Token.ID)
	if err != nil {
		t.Fatalf("get rotated: %v", err)
	}
	if stored.Enabled {
		t.Fatalf("stored rotated row is enabled; a disabled token must stay disabled across rotation")
	}
	if got, err := ts.LookupReadOnly(rotated.Plaintext); err == nil && got != nil {
		t.Fatalf("rotated replacement of a disabled token authenticates")
	}
	// The flag stays reversible: re-enabling the replacement makes it work.
	if err := ts.SetEnabled(rotated.Token.ID, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if got, err := ts.LookupReadOnly(rotated.Plaintext); err != nil || got == nil {
		t.Fatalf("re-enabled replacement does not authenticate: %v", err)
	}
}

// An explicit null expiry is the documented way to make the replacement
// non-expiring (docs/openapi.yaml rotate body); omitting it keeps the expiry.
func TestRotateExplicitNullExpiryClearsExpiry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTokenStore(db).WithCipher(newTestCipher(t))
	c, err := repo.Create(Client{Name: "carol", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	expiry := time.Now().Add(time.Hour).Unix()
	issued, err := ts.Issue(c.ID, "tv", &expiry)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rotated, err := ts.RotateWithExpiry(issued.Token.ID, nil, true)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	stored, err := ts.Get(rotated.Token.ID)
	if err != nil {
		t.Fatalf("get rotated: %v", err)
	}
	if stored.ExpiresAt != nil {
		t.Fatalf("explicit null expiry kept %d", *stored.ExpiresAt)
	}
}
