package api

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// bcryptFloor bounds how fast a cost-10 bcrypt comparison can possibly
// complete: the work factor alone guarantees multiple milliseconds on any
// hardware, while a skipped comparison returns in microseconds. The check
// therefore separates "compare ran" from "compare was skipped" without
// depending on wall-clock precision.
const bcryptFloor = 5 * time.Millisecond

// TestDummyLoginPasswordHashIsRealBcrypt (#1064): the timing-equalization
// hash must itself be a valid bcrypt hash at production cost, otherwise the
// unknown-user compare degrades (or is silently cheap) and the oracle
// returns.
func TestDummyLoginPasswordHashIsRealBcrypt(t *testing.T) {
	cost, err := bcrypt.Cost(dummyLoginPasswordHash)
	if err != nil {
		t.Fatalf("dummyLoginPasswordHash is not a bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost=%d, want %d so unknown users pay the same compare cost", cost, bcrypt.DefaultCost)
	}
}

// TestUnknownUsernamePaysBcryptCost (#1064): a login for a username that maps
// to no account must still run a bcrypt comparison — without it the login
// response time reveals whether the account exists.
func TestUnknownUsernamePaysBcryptCost(t *testing.T) {
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, "secure-password-123"),
		Role:         "admin",
	})
	snapshot := state.snapshotLoginCredentials("mallory")
	if snapshot.FoundUser {
		t.Fatal("snapshot unexpectedly found the unknown user")
	}
	start := time.Now()
	if snapshot.passwordMatches("supplied-password") {
		t.Fatal("unknown username must never authenticate")
	}
	if elapsed := time.Since(start); elapsed < bcryptFloor {
		t.Fatalf("unknown-user check returned in %s — bcrypt was skipped and the login path is an account-enumeration oracle", elapsed)
	}
}

// TestFallbackLoginPaysBcryptCost (#1064): the empty-panel admin fallback is
// a constant-time string compare — fast by itself — so it too must be
// preceded by the dummy bcrypt compare or first-boot probing would measure
// the difference.
func TestFallbackLoginPaysBcryptCost(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		settings: Settings{NaivePassword: "fallback-password-123", PanelAccess: "local"},
	}
	snapshot := state.snapshotLoginCredentials("admin")
	if !snapshot.FallbackAllowed {
		t.Fatal("expected fallback-allowed snapshot for empty user list")
	}
	start := time.Now()
	if !snapshot.passwordMatches("fallback-password-123") {
		t.Fatal("fallback password must still authenticate")
	}
	if elapsed := time.Since(start); elapsed < bcryptFloor {
		t.Fatalf("fallback check returned in %s — bcrypt was skipped", elapsed)
	}
}

// TestExistingUserLoginStaysBcryptBound (#1064): the positive control — an
// existing user check already pays bcrypt; the test exists so a refactor
// cannot flip the unknown-user branch back to the fast path unnoticed. The
// fixture hash uses DefaultCost (not the MinCost test helper) so the compare
// duration sits far above the floor.
func TestExistingUserLoginStaysBcryptBound(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secure-password-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	state := loginReliabilityState(t, User{
		Username:     "alice",
		PasswordHash: string(hash),
		Role:         "admin",
	})
	snapshot := state.snapshotLoginCredentials("alice")
	start := time.Now()
	snapshot.passwordMatches("wrong-password")
	if elapsed := time.Since(start); elapsed < bcryptFloor {
		t.Fatalf("existing-user wrong-password check returned in %s — bcrypt compare missing", elapsed)
	}
}
