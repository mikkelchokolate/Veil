package client

import (
	"strings"
	"testing"
)

// TestRuntimeIdentityLowercaseOnly (#1111): hysteria2 lowercases the username
// it reports for accounting, so a stored mixed-case identity can never match
// its counters, and "Alice"/"alice" collapse into one runtime user. The
// domain must reject non-lowercase identities.
func TestRuntimeIdentityLowercaseOnly(t *testing.T) {
	for _, value := range []string{"Alice", "alice-Upper", "USER_1", "v_ABC"} {
		if err := ValidateRuntimeIdentity(value); err == nil {
			t.Fatalf("ValidateRuntimeIdentity(%q): want rejection", value)
		}
	}
	for _, value := range []string{"alice", "user-1", "v_0123456789abcdef", "a_b"} {
		if err := ValidateRuntimeIdentity(value); err != nil {
			t.Fatalf("ValidateRuntimeIdentity(%q): %v", value, err)
		}
	}
}

// TestCreateBindingRejectsMixedCaseRuntimeIdentity (#1111): the write path —
// not only the HTTP layer — enforces the canonical form so a case-only
// collision cannot be created through any caller.
func TestCreateBindingRejectsMixedCaseRuntimeIdentity(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, err := repo.Create(Client{Name: "case-check", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", RuntimeIdentity: "MiXeD", Enabled: true}); err == nil {
		t.Fatal("mixed-case runtime identity accepted")
	}
}

// TestGenerateRuntimeIdentityIsBoundedLowercase (#1140): migration 7 derived
// v_<full id> without the 32-char truncation this generator applies; both
// paths must derive the same value or rendering and accounting diverge.
func TestGenerateRuntimeIdentityIsBoundedLowercase(t *testing.T) {
	id := strings.ToUpper("01234567-89ab-cdef-0123-456789abcdef")
	got := GenerateRuntimeIdentity(id)
	if len(got) > 34 || got != strings.ToLower(got) {
		t.Fatalf("identity %q not bounded lowercase", got)
	}
	if err := ValidateRuntimeIdentity(got); err != nil {
		t.Fatalf("generated identity invalid: %v", err)
	}
	want := "v_" + strings.ToLower(strings.ReplaceAll(id, "-", ""))[:32]
	if got != want {
		t.Fatalf("identity %q, want %q", got, want)
	}
}
