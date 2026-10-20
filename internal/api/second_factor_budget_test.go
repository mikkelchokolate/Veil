package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// totpPrefixRequest addresses each call from its own IPv6 /64, so every
// per-(client, username) limiter and backoff key is fresh — exactly the
// prefix-rotation shape of the #1286 probe.
func totpPrefixRequest(method, path, body string, prefix int) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = fmt.Sprintf("[2001:db8:%x:%x::1]:443", prefix>>16, prefix&0xffff)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func totpLoginFromPrefix(t *testing.T, state *managementState, password string, prefix int) string {
	t.Helper()
	rec := httptest.NewRecorder()
	state.handleLoginWithRevalidation(rec, totpPrefixRequest(http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"`+password+`"}`, prefix))
	pending := cookieValue(rec, pendingSecondFactorCookie)
	if pending == "" {
		t.Fatalf("login from prefix %d: no pending challenge (status=%d body=%s)", prefix, rec.Code, rec.Body.String())
	}
	return pending
}

func totpVerifyFromPrefix(state *managementState, pending, body string, prefix int) *httptest.ResponseRecorder {
	req := totpPrefixRequest(http.MethodPost, "/api/v1/auth/totp/verify", body, prefix)
	req.AddCookie(&http.Cookie{Name: pendingSecondFactorCookie, Value: pending})
	rec := httptest.NewRecorder()
	state.handleTOTPVerify(rec, req)
	return rec
}

// TestTOTPVerifyGlobalGuessBudgetSpansClientPrefixes is the #1286 probe as a
// regression: rotating /64s and re-minting challenges with the known password
// must NOT buy fresh second-factor guesses. Once the process-wide
// per-username failure budget is spent, verify is rejected outright — even
// for the correct code — until the budget refills.
func TestTOTPVerifyGlobalGuessBudgetSpansClientPrefixes(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = secret
	state, now := totpTestState(t, user)

	prefix := 0
	next := func() int { prefix++; return prefix }

	failures := 0
	for failures < secondFactorGuessBudget {
		pending := totpLoginFromPrefix(t, state, "correct-password-123", next())
		for i := 0; i < pendingSecondFactorMaxAttempts-1 && failures < secondFactorGuessBudget; i++ {
			rec := totpVerifyFromPrefix(state, pending, `{"code":"000000"}`, next())
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("guess %d status=%d body=%s, want 401 while the budget lasts", failures, rec.Code, rec.Body.String())
			}
			failures++
		}
	}

	// Budget spent: a fresh challenge from a fresh prefix gets no more guesses.
	pending := totpLoginFromPrefix(t, state, "correct-password-123", next())
	rec := totpVerifyFromPrefix(state, pending, `{"code":"000000"}`, next())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget guess status=%d body=%s, want 429", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("over-budget rejection carries no Retry-After")
	}
	// Rejected, not evaluated: even the correct code mints nothing.
	rec = totpVerifyFromPrefix(state, pending, `{"code":"`+totpCode(t, secret, *now)+`"}`, next())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget correct code status=%d body=%s, want 429", rec.Code, rec.Body.String())
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted while the per-username second-factor budget was exhausted")
	}
	// Recovery codes draw on the same budget.
	rec = totpVerifyFromPrefix(state, pending, `{"recoveryCode":"AAAA-BBBB"}`, next())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget recovery code status=%d, want 429", rec.Code)
	}

	// The budget refills over time: one refill interval later the honest
	// operator completes login with a correct code.
	*now = now.Add(secondFactorGuessRefill + time.Second)
	pending = totpLoginFromPrefix(t, state, "correct-password-123", next())
	rec = totpVerifyFromPrefix(state, pending, `{"code":"`+totpCode(t, secret, *now)+`"}`, next())
	if rec.Code != http.StatusOK {
		t.Fatalf("post-refill verify status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
}

// TestSecondFactorGuessBudgetReservation pins the budget mechanics: the
// reservation is taken atomically before a guess is evaluated (so concurrent
// guesses cannot overrun it), refunded for non-failures, scoped per folded
// username, and refilled linearly.
func TestSecondFactorGuessBudgetReservation(t *testing.T) {
	state, now := totpTestState(t, totpTestUser(t, "correct-password-123"))

	// Concurrent reservations never exceed the budget.
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < secondFactorGuessBudget*3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, ok := state.reserveSecondFactorGuess("alice"); ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != secondFactorGuessBudget {
		t.Fatalf("granted %d concurrent reservations, want exactly %d", granted, secondFactorGuessBudget)
	}
	_, retryAfter, ok := state.reserveSecondFactorGuess("  ALICE ")
	if ok {
		t.Fatal("case/space-variant username minted a fresh budget")
	}
	if retryAfter <= 0 || retryAfter > secondFactorGuessRefill {
		t.Fatalf("retryAfter=%v, want (0, %v]", retryAfter, secondFactorGuessRefill)
	}

	// Another account is unaffected.
	refund, _, ok := state.reserveSecondFactorGuess("bob")
	if !ok {
		t.Fatal("alice's exhausted budget blocked bob")
	}
	refund()

	// One refill interval restores exactly one guess; a refund returns it.
	*now = now.Add(secondFactorGuessRefill)
	refund, _, ok = state.reserveSecondFactorGuess("alice")
	if !ok {
		t.Fatal("budget did not refill after one interval")
	}
	if _, _, ok := state.reserveSecondFactorGuess("alice"); ok {
		t.Fatal("one refill interval granted more than one guess")
	}
	refund()
	refund() // idempotent: a double refund must not mint an extra guess
	if _, _, ok := state.reserveSecondFactorGuess("alice"); !ok {
		t.Fatal("refunded reservation was not returned to the budget")
	}
	if _, _, ok := state.reserveSecondFactorGuess("alice"); ok {
		t.Fatal("double refund minted an extra guess")
	}
}

// TestTOTPVerifySuccessDoesNotSpendGuessBudget: only wrong guesses draw on
// the per-username budget — a successful factor is refunded, so an operator
// who signs in between someone else's failures is never charged for it.
func TestTOTPVerifySuccessDoesNotSpendGuessBudget(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	user.TOTPRecoveryHashes = []string{hashRecoveryCode("ABCD-EFGH")}
	state, _ := totpTestState(t, user)

	prefix := 1000
	next := func() int { prefix++; return prefix }
	guess := func(pending string) int {
		return totpVerifyFromPrefix(state, pending, `{"code":"000000"}`, next()).Code
	}

	failures := 0
	for failures < secondFactorGuessBudget-1 {
		pending := totpLoginFromPrefix(t, state, "correct-password-123", next())
		for i := 0; i < pendingSecondFactorMaxAttempts-1 && failures < secondFactorGuessBudget-1; i++ {
			if code := guess(pending); code != http.StatusUnauthorized {
				t.Fatalf("guess %d status=%d, want 401", failures, code)
			}
			failures++
		}
	}
	// One guess left: a successful recovery-code login must not spend it.
	pending := totpLoginFromPrefix(t, state, "correct-password-123", next())
	if rec := totpVerifyFromPrefix(state, pending, `{"recoveryCode":"ABCD-EFGH"}`, next()); rec.Code != http.StatusOK {
		t.Fatalf("recovery verify status=%d body=%s", rec.Code, rec.Body.String())
	}
	pending = totpLoginFromPrefix(t, state, "correct-password-123", next())
	if code := guess(pending); code != http.StatusUnauthorized {
		t.Fatalf("last budgeted guess status=%d, want 401 (success must be refunded)", code)
	}
	if code := guess(pending); code != http.StatusTooManyRequests {
		t.Fatalf("over-budget guess status=%d, want 429", code)
	}
}
