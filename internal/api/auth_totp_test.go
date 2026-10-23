package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// totpTestState builds a management state with an injectable auth clock: the
// same clock steers login backoff, pending_2fa expiry, and TOTP validation so
// tests control the whole second-factor timeline from one variable.
func totpTestState(t *testing.T, user User) (*managementState, *time.Time) {
	t.Helper()
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state := &managementState{
		statePath: filepath.Join(t.TempDir(), "state.json"),
		sessions:  registry,
		users:     []User{user},
	}
	state.loginBackoffNow = func() time.Time { return now }
	return state, &now
}

func totpTestUser(t *testing.T, password string) User {
	t.Helper()
	return User{
		Username:     "alice",
		PasswordHash: loginReliabilityPasswordHash(t, password),
		Role:         "admin",
		Locale:       "en",
	}
}

func totpCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func totpLogin(t *testing.T, state *managementState, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	state.handleLoginWithRevalidation(rec, req)
	return rec
}

func cookieValue(rec *httptest.ResponseRecorder, name string) string {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

var totpVerifyCall int

func totpVerify(t *testing.T, state *managementState, pendingToken, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp/verify", strings.NewReader(body))
	// The per-(client, username) verify budget is a real-clock token bucket
	// (burst 3); giving each call a fresh RemoteAddr keeps these tests on the
	// per-challenge attempt cap they are exercising rather than the bucket.
	totpVerifyCall++
	req.RemoteAddr = fmt.Sprintf("192.0.2.%d:443", 1+totpVerifyCall%200)
	req.Header.Set("Content-Type", "application/json")
	if pendingToken != "" {
		req.AddCookie(&http.Cookie{Name: pendingSecondFactorCookie, Value: pendingToken})
	}
	rec := httptest.NewRecorder()
	state.handleTOTPVerify(rec, req)
	return rec
}

func authedTOTPRequest(t *testing.T, state *managementState, session Session, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, session.Username))
	rec := httptest.NewRecorder()
	switch path {
	case "/api/v1/users/me/totp":
		state.handleMyTOTP(rec, req)
	case "/api/v1/users/me/totp/enroll":
		state.handleMyTOTPEnroll(rec, req)
	case "/api/v1/users/me/totp/confirm":
		state.handleMyTOTPConfirm(rec, req)
	default:
		t.Fatalf("no handler mapped for %s", path)
	}
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v (raw %q)", err, rec.Body.String())
	}
	return body
}

func TestTOTPLoginMintsPendingChallengeNotSession(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, _ := totpTestState(t, user)

	rec := totpLogin(t, state, "correct-password-123")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["secondFactorRequired"] != true {
		t.Fatalf("expected secondFactorRequired, got %v", body)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("login minted a session before the second factor was verified")
	}
	pending := cookieValue(rec, pendingSecondFactorCookie)
	if pending == "" {
		t.Fatal("no pending_2fa cookie issued")
	}
}

func TestTOTPVerifyCompletesLogin(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	if pending == "" {
		t.Fatal("no pending_2fa cookie issued")
	}
	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	rec := totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", rec.Code, rec.Body.String())
	}
	token := cookieValue(rec, "veil_session")
	if token == "" {
		t.Fatal("verify did not mint the session cookie")
	}
	if cookieValue(rec, pendingSecondFactorCookie) != "" {
		t.Fatal("pending cookie was not cleared")
	}
	session, ok := state.sessionRegistry().Get(token)
	if !ok || !session.SecondFactor {
		t.Fatalf("session missing or unmarked: ok=%v session=%+v", ok, session)
	}
	// The challenge is single-use: a second verify on the same token fails.
	rec = totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed challenge status=%d, want 401", rec.Code)
	}
}

func TestTOTPVerifyRejectsWrongCodeAndCapsAttempts(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	for i := 0; i < pendingSecondFactorMaxAttempts; i++ {
		rec := totpVerify(t, state, pending, `{"code":"000000"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d, want 401", i, rec.Code)
		}
		// Step past the exponential backoff without expiring the challenge.
		*now = now.Add(9 * time.Second)
	}
	// The challenge is destroyed after the cap: even a correct code fails.
	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	rec := totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("exhausted challenge status=%d, want 401", rec.Code)
	}
}

func TestTOTPPendingChallengeExpires(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	*now = now.Add(pendingSecondFactorTTL + time.Second)
	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	rec := totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired challenge status=%d, want 401", rec.Code)
	}
	if cookieValue(rec, pendingSecondFactorCookie) != "" {
		t.Fatal("expired challenge cookie was not cleared")
	}
}

func TestTOTPVerifyFailsClosedOnPasswordChange(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	state.mu.Lock()
	state.users[0].PasswordHash = loginReliabilityPasswordHash(t, "rotated-password-456")
	state.mu.Unlock()

	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	rec := totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale-credential challenge status=%d, want 401", rec.Code)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted against rotated credentials")
	}
}

func TestTOTPRecoveryCodeSingleUse(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	user.TOTPRecoveryHashes = []string{hashRecoveryCode("ABCD-EFGH")}
	state, now := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	rec := totpVerify(t, state, pending, `{"recoveryCode":"abcd-efgh"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery verify status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := state.users[0].TOTPRecoveryHashes; len(got) != 0 {
		t.Fatalf("recovery hash was not consumed: %v", got)
	}

	// Same code again must not verify even on a fresh challenge.
	*now = now.Add(time.Second)
	pending = cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	rec = totpVerify(t, state, pending, `{"recoveryCode":"ABCD-EFGH"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed recovery code status=%d, want 401", rec.Code)
	}
}

func TestTOTPEnrollmentLifecycle(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	// Status starts disabled with no pending enrollment.
	rec := authedTOTPRequest(t, state, session, http.MethodGet, "/api/v1/users/me/totp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["enabled"] != false || body["pendingEnrollment"] != false {
		t.Fatalf("unexpected status %v", body)
	}

	// Enroll returns a pending secret; the factor is not yet enabled.
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"correct-password-123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	secret, _ := body["secret"].(string)
	uri, _ := body["otpauthUri"].(string)
	if secret == "" || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("enroll response=%v", body)
	}
	if state.users[0].TOTPEnabled {
		t.Fatal("enroll enabled the factor before confirm")
	}

	// Wrong confirm code is a bounded, throttled failure.
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"000000"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad confirm status=%d, want 400", rec.Code)
	}
	*now = now.Add(2 * time.Second)

	// Correct code confirms: factor on, ~10 recovery codes, session upgraded.
	code := totpCode(t, secret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	codes, _ := body["recoveryCodes"].([]any)
	if body["enabled"] != true || len(codes) != recoveryCodeCount {
		t.Fatalf("confirm response=%v", body)
	}
	if !state.users[0].TOTPEnabled || state.users[0].TOTPSecret == "" || state.users[0].TOTPPendingSecret != "" {
		t.Fatalf("user state after confirm=%+v", state.users[0])
	}
	if len(state.users[0].TOTPRecoveryHashes) != recoveryCodeCount {
		t.Fatalf("recovery hashes=%v", state.users[0].TOTPRecoveryHashes)
	}
	marked, ok := state.sessionRegistry().Get(session.Token)
	if !ok || !marked.SecondFactor {
		t.Fatalf("enrolling session was not upgraded: %+v", marked)
	}

	// Disable with a live authenticator code clears every second-factor
	// field (factor-grade contract — password alone is rejected). Advance
	// one timestep: reusing the confirm step's code is now a replay and is
	// correctly rejected (#1220).
	*now = now.Add(30 * time.Second)
	disableCode := totpCode(t, state.users[0].TOTPSecret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{"code":"`+disableCode+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].HasUsableTOTPSecret() {
		t.Fatalf("disable left TOTP state: %+v", state.users[0])
	}
}

func TestTOTPConfirmRevokesPreEnrollmentSessions(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, now := totpTestState(t, user)
	enrolling := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	stale := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	authedTOTPRequest(t, state, enrolling, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"correct-password-123"}`)
	secret := state.users[0].TOTPPendingSecret
	code := totpCode(t, secret, *now)
	rec := authedTOTPRequest(t, state, enrolling, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(stale.Token); ok {
		t.Fatal("pre-enrollment session survived TOTP activation")
	}
	if _, ok := state.sessionRegistry().Get(enrolling.Token); !ok {
		t.Fatal("enrolling session was revoked")
	}
}

func TestTOTPDisableRejectsWrongCode(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	rec := authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{"code":"000000"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable with wrong code status=%d, want 400", rec.Code)
	}
	if !state.users[0].TOTPEnabled {
		t.Fatal("disable succeeded with a wrong code")
	}

	// An empty body is rejected the same way — the code is mandatory.
	rec = authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable with no code status=%d, want 400", rec.Code)
	}
	if !state.users[0].TOTPEnabled {
		t.Fatal("disable succeeded without a code")
	}
}

// Disabling the factor must be factor-grade (#1172 review): a stolen
// 2FA-complete session plus the account password must not be enough. The
// live TOTP code is the only self-service path; losing the authenticator
// entirely goes through admin reset.
func TestTOTPDisableRejectsPasswordAlone(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	rec := authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{"password":"correct-password-123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable with password alone status=%d, want 400", rec.Code)
	}
	if !state.users[0].TOTPEnabled {
		t.Fatal("password alone disabled the factor")
	}

	code := totpCode(t, user.TOTPSecret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable with live code status=%d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPEnabled {
		t.Fatal("live TOTP code did not disable the factor")
	}
}

// A confirmed factor whose enrolling session is already gone (expired or
// revoked between enroll and confirm) must roll back the enrollment instead
// of leaving TOTP enabled on an unmarked session (#1172 review).
func TestTOTPConfirmRollsBackWhenEnrollingSessionGone(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"correct-password-123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status=%d", rec.Code)
	}
	pendingSecret := state.users[0].TOTPPendingSecret
	if pendingSecret == "" {
		t.Fatal("enroll produced no pending secret")
	}

	if _, err := state.sessionRegistry().DeleteTokenPersisted(session.Token); err != nil {
		t.Fatalf("delete enrolling session: %v", err)
	}

	code := totpCode(t, pendingSecret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("confirm with gone session status=%d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPEnabled {
		t.Fatal("factor enabled while its enrolling session was unmarkable")
	}
	if state.users[0].TOTPPendingSecret == "" {
		t.Fatal("rollback dropped the pending enrollment")
	}
}

// The in-flight lease on a pending challenge must be exclusive (#1172
// review): two concurrent verifies on one pending cookie cannot both
// validate one code into two sessions.
func TestPendingSecondFactorClaimIsExclusive(t *testing.T) {
	store := newPendingSecondFactorStore(time.Now)
	token, _, err := store.Issue("alice", "hash")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, res := store.Claim(token); res != pendingClaimOK {
		t.Fatal("first claim rejected")
	}
	if _, res := store.Claim(token); res != pendingClaimBusy {
		t.Fatalf("second concurrent claim: got %v, want busy", res)
	}
	store.Release(token)
	if _, res := store.Claim(token); res != pendingClaimOK {
		t.Fatal("claim after release rejected")
	}
	store.Consume(token)
	if _, res := store.Claim(token); res != pendingClaimMissing {
		t.Fatalf("claim on consumed challenge: got %v, want missing", res)
	}
}

func TestTOTPAdminResetClearsFactorAndSessions(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	user.TOTPRecoveryHashes = []string{hashRecoveryCode("ABCD-EFGH")}
	state, _ := totpTestState(t, user)
	userSession := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/totp", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleV1UserTOTPReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].HasUsableTOTPSecret() {
		t.Fatalf("reset left TOTP state: %+v", state.users[0])
	}
	if _, ok := state.sessionRegistry().Get(userSession.Token); ok {
		t.Fatal("user session survived admin reset")
	}

	// Reset is idempotent.
	rec = httptest.NewRecorder()
	state.handleV1UserTOTPReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent reset status=%d", rec.Code)
	}

	// A viewer cannot reset another user's factor.
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state.users[0] = user
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/totp", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleV1UserTOTPReset(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer reset status=%d, want 403", rec.Code)
	}
}

func TestTOTPSessionWithoutFactorMarkIsRevoked(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	// Pre-enrollment sessions are accepted while the factor is off. This
	// drives authorizeCookieSession, the rule the middleware, the status
	// endpoint and SSE actually run.
	if _, _, ok := state.authorizeCookieSession(session.Token, session); !ok {
		t.Fatal("session rejected before TOTP enrollment")
	}
	state.mu.Lock()
	state.users[0].TOTPEnabled = true
	state.mu.Unlock()
	if _, _, ok := state.authorizeCookieSession(session.Token, session); ok {
		t.Fatal("pre-enrollment session still accepted after TOTP enabled")
	}
	if _, ok := state.sessionRegistry().Get(session.Token); ok {
		t.Fatal("pre-enrollment session was not revoked after TOTP enabled")
	}
}

func TestPendingChallengeCookieIsNotASession(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, _ := totpTestState(t, user)

	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	if pending == "" {
		t.Fatal("no pending_2fa cookie issued")
	}
	// The pending token must resolve through the factor store only — it is
	// not registered as a session.
	if _, ok := state.sessionRegistry().Get(pending); ok {
		t.Fatal("pending_2fa token resolved as a session")
	}
}

// RFC 6238 §5.2 (#1220): an accepted code must never verify twice inside
// its ±1-step validity window. The verify path records the consumed
// timestep on the account, a fresh challenge presenting the same code is
// rejected, and the NEXT step's code still verifies.
func TestTOTPVerifyRejectsReplayedStepAcrossChallenges(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)

	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	rec := totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("first verify status=%d body=%s", rec.Code, rec.Body.String())
	}
	if want := (*now).Unix() / totpPeriod; state.users[0].TOTPLastStep != want {
		t.Fatalf("verify watermark=%d, want matched step %d", state.users[0].TOTPLastStep, want)
	}

	// Fresh login, SAME code inside its window: rejected as replayed and
	// no session is minted.
	pending = cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	if pending == "" {
		t.Fatal("second login minted no pending challenge")
	}
	rec = totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code status=%d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted for a replayed code")
	}

	// The next timestep's code verifies — the watermark blocks only what
	// was already consumed.
	*now = now.Add(totpPeriod * time.Second)
	pending = cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	rec = totpVerify(t, state, pending, `{"code":"`+totpCode(t, "JBSWY3DPEHPK3PXP", *now)+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("next-step verify status=%d body=%s", rec.Code, rec.Body.String())
	}
	if want := (*now).Unix() / totpPeriod; state.users[0].TOTPLastStep != want {
		t.Fatalf("watermark after next-step verify=%d, want %d", state.users[0].TOTPLastStep, want)
	}
}

// The confirming code is spent at confirm time and the disabling code's
// step is persisted while the rest of the factor clears (#1220): neither
// ceremony leaves a code replayable for login, and teardown cannot rewind
// the monotonic watermark.
func TestTOTPConfirmAndDisableAdvanceReplayWatermark(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"correct-password-123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status=%d", rec.Code)
	}
	pendingSecret := state.users[0].TOTPPendingSecret
	code := totpCode(t, pendingSecret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	if want := (*now).Unix() / totpPeriod; state.users[0].TOTPLastStep != want {
		t.Fatalf("confirm watermark=%d, want %d", state.users[0].TOTPLastStep, want)
	}

	// The code that armed the factor cannot complete a fresh login.
	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	rec = totpVerify(t, state, pending, `{"code":"`+code+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("confirm code replayed into login status=%d, want 401", rec.Code)
	}

	// Disable spends its own step: the watermark survives ClearTOTP while
	// every other factor field is dropped.
	*now = now.Add(2 * totpPeriod * time.Second)
	disableCode := totpCode(t, state.users[0].TOTPSecret, *now)
	rec = authedTOTPRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/totp", `{"code":"`+disableCode+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPEnabled || state.users[0].HasUsableTOTPSecret() {
		t.Fatalf("disable left TOTP state: %+v", state.users[0])
	}
	if want := (*now).Unix() / totpPeriod; state.users[0].TOTPLastStep != want {
		t.Fatalf("disable watermark=%d, want %d", state.users[0].TOTPLastStep, want)
	}
}

// Admin reset clears the factor but never the monotonic watermark: a code
// spent under the old secret must stay spent if the same material were
// ever re-armed (#1220).
func TestTOTPAdminResetPreservesReplayWatermark(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	user.TOTPLastStep = 4242
	state, _ := totpTestState(t, user)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/totp", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleV1UserTOTPReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].HasUsableTOTPSecret() {
		t.Fatal("reset left TOTP state")
	}
	if got := state.users[0].TOTPLastStep; got != 4242 {
		t.Fatalf("reset rewound the replay watermark: %d", got)
	}
}

// validateTOTPCode reports the matched timestep for every code inside the
// ±1 skew window so callers can record it (#1220).
func TestValidateTOTPCodeReturnsMatchedStep(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	current := now.Unix() / totpPeriod
	for _, offset := range []int64{0, 1, -1} {
		code := totpCode(t, secret, now.Add(time.Duration(offset)*totpPeriod*time.Second))
		step, ok := validateTOTPCode(now, secret, code)
		if !ok || step != current+offset {
			t.Fatalf("offset %d: step=%d ok=%v, want step %d", offset, step, ok, current+offset)
		}
	}
	if _, ok := validateTOTPCode(now, secret, "000000"); ok {
		t.Fatal("bogus code validated")
	}
	if _, ok := validateTOTPCode(now, "", totpCode(t, secret, now)); ok {
		t.Fatal("empty secret validated")
	}
}

// A stolen cookie session on a factor-less account must not be able to mint
// a TOTP seed (#1250): enroll is the factor-creating step, so it carries the
// same credential-grade gate as passkey registration (#1232). Without the
// password the attacker gets no seed, no pending secret is stored, and the
// owner's other sessions survive.
func TestTOTPEnrollRequiresPasswordOnUnmarkedSession(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	stolen := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	owner := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	for name, body := range map[string]string{
		"no body":        "",
		"empty password": `{"password":"   "}`,
		"wrong password": `{"password":"not-the-password"}`,
	} {
		rec := authedTOTPRequest(t, state, stolen, http.MethodPost, "/api/v1/users/me/totp/enroll", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: enroll status=%d body=%s, want 400", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "otpauth") || strings.Contains(rec.Body.String(), `"secret"`) {
			t.Fatalf("%s: gated enroll leaked a seed: %s", name, rec.Body.String())
		}
		if state.users[0].TOTPPendingSecret != "" || state.users[0].TOTPEnabled {
			t.Fatalf("%s: gated enroll mutated TOTP state: %+v", name, state.users[0])
		}
	}

	// Confirm has nothing to activate: the only path to a pending secret
	// is the gated enroll.
	rec := authedTOTPRequest(t, state, stolen, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"123456"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm without enrollment status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(owner.Token); !ok {
		t.Fatal("owner session revoked by a gated enroll attempt")
	}
	if state.users[0].TOTPEnabled {
		t.Fatal("TOTP armed without a credential-grade proof")
	}
}

// Wrong passwords at the enroll gate share the login backoff family so the
// gate cannot be ground as a password oracle (#1250).
func TestTOTPEnrollWrongPasswordIsThrottled(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"wrong-1"}`)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("wrong password status=%d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Even the correct password is refused while the backoff is active.
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"password":"correct-password-123"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("enroll inside backoff status=%d body=%s, want 429", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPPendingSecret != "" {
		t.Fatal("throttled enroll stored a pending secret")
	}
}

// A session that already completed a second factor keeps the passwordless
// enroll path on an account with no active TOTP factor, matching
// passkeyCredentialGate (#1250). Replacing an ACTIVE factor additionally needs
// a live code from it (#1413), after which the old factor still stays live
// until the new secret proves itself at confirm.
func TestTOTPEnrollAllowsSecondFactorMarkedSessionWithoutPassword(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if marked, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil || !marked {
		t.Fatalf("mark second factor: marked=%v err=%v", marked, err)
	}

	code := totpCode(t, "JBSWY3DPEHPK3PXP", *now)
	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("marked-session enroll status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPPendingSecret == "" {
		t.Fatal("marked-session enroll stored no pending secret")
	}
	if state.users[0].TOTPSecret != "JBSWY3DPEHPK3PXP" || !state.users[0].TOTPEnabled {
		t.Fatal("re-enroll replaced the active factor before confirm")
	}
	if want := (*now).Unix() / totpPeriod; state.users[0].TOTPLastStep != want {
		t.Fatalf("re-enroll did not spend the proving code: watermark=%d, want %d", state.users[0].TOTPLastStep, want)
	}
}

// A marked session on an account without an active TOTP factor (for example
// a passkey-only account) still enrolls with no password and no code:
// adding a first TOTP factor replaces nothing (#1250 contract kept by #1413).
func TestTOTPFirstEnrollOnMarkedSessionNeedsNoCode(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if marked, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil || !marked {
		t.Fatalf("mark second factor: marked=%v err=%v", marked, err)
	}
	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("first enroll status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPPendingSecret == "" {
		t.Fatal("first enroll stored no pending secret")
	}
}

// A stolen second-factor-marked session (with or without the password) must
// not be able to replace an active TOTP factor and its recovery codes: that is
// a stronger action than disable, which already demands a live code (#1413).
// Without a valid, unspent code from the current authenticator the enroll
// mints no seed, so confirm has nothing to activate, the owner's factor,
// recovery codes and sessions all survive.
func TestTOTPReenrollOverActiveFactorRequiresCurrentCode(t *testing.T) {
	const activeSecret = "JBSWY3DPEHPK3PXP"
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = activeSecret
	user.TOTPRecoveryHashes = []string{hashRecoveryCode("AAAA-BBBB")}
	state, now := totpTestState(t, user)
	stolen := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(stolen.Token); err != nil {
		t.Fatal(err)
	}
	owner := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(owner.Token); err != nil {
		t.Fatal(err)
	}

	// The current step is spent already (e.g. by the owner's last login):
	// replaying it is refused like a wrong code.
	spent := totpCode(t, activeSecret, *now)
	state.users[0].TOTPLastStep = (*now).Unix() / totpPeriod

	for name, body := range map[string]string{
		"no body":             "",
		"password only":       `{"password":"correct-password-123"}`,
		"blank code":          `{"code":"   "}`,
		"recovery code":       `{"code":"AAAA-BBBB"}`,
		"spent code replayed": `{"code":"` + spent + `"}`,
	} {
		rec := authedTOTPRequest(t, state, stolen, http.MethodPost, "/api/v1/users/me/totp/enroll", body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: enroll status=%d body=%s, want 400/429", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "otpauth") || strings.Contains(rec.Body.String(), `"secret"`) {
			t.Fatalf("%s: gated re-enroll leaked a seed: %s", name, rec.Body.String())
		}
		if state.users[0].TOTPPendingSecret != "" {
			t.Fatalf("%s: gated re-enroll stored a pending secret", name)
		}
	}

	// Confirm has no attacker-controlled pending secret to activate.
	rec := authedTOTPRequest(t, state, stolen, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"123456"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm without enrollment status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := state.users[0]
	if !got.TOTPEnabled || got.TOTPSecret != activeSecret {
		t.Fatalf("active factor replaced: %+v", got)
	}
	if _, ok := consumeRecoveryCodeHash(got.TOTPRecoveryHashes, "AAAA-BBBB"); !ok {
		t.Fatal("owner's recovery code no longer valid")
	}
	if _, ok := state.sessionRegistry().Get(owner.Token); !ok {
		t.Fatal("owner session revoked by a gated re-enroll attempt")
	}
}

// Wrong codes at the re-enroll gate are throttled through the login backoff
// family, so the enroll form is not a code oracle for the active factor
// (#1413). Even a correct code is refused inside the backoff window.
func TestTOTPReenrollWrongCodeIsThrottled(t *testing.T) {
	const activeSecret = "JBSWY3DPEHPK3PXP"
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = activeSecret
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil {
		t.Fatal(err)
	}
	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"code":"000000"}`)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("wrong code status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"code":"`+totpCode(t, activeSecret, *now)+`"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("enroll inside backoff status=%d body=%s, want 429", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPPendingSecret != "" {
		t.Fatal("throttled re-enroll stored a pending secret")
	}
}

// The owner who holds the current authenticator can still rotate it: a live
// code at enroll, then a code from the new seed at confirm. The proving code
// is spent, so it can neither complete a login nor disable the new factor.
func TestTOTPReenrollWithCurrentCodeRotatesFactor(t *testing.T) {
	const activeSecret = "JBSWY3DPEHPK3PXP"
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = activeSecret
	user.TOTPRecoveryHashes = []string{hashRecoveryCode("AAAA-BBBB")}
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil {
		t.Fatal(err)
	}

	proof := totpCode(t, activeSecret, *now)
	rec := authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"code":"`+proof+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", rec.Code, rec.Body.String())
	}
	newSecret, _ := decodeBody(t, rec)["secret"].(string)
	if newSecret == "" || newSecret == activeSecret {
		t.Fatalf("enroll returned secret %q", newSecret)
	}

	// The same proving code cannot be reused for a second seed.
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/enroll", `{"code":"`+proof+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("replayed proof status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if state.users[0].TOTPPendingSecret != newSecret {
		t.Fatal("replayed proof replaced the pending secret")
	}

	*now = now.Add(totpPeriod * time.Second)
	rec = authedTOTPRequest(t, state, session, http.MethodPost, "/api/v1/users/me/totp/confirm", `{"code":"`+totpCode(t, newSecret, *now)+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := state.users[0]
	if !got.TOTPEnabled || got.TOTPSecret != newSecret || got.TOTPPendingSecret != "" {
		t.Fatalf("rotation did not activate the new secret: %+v", got)
	}
	if _, ok := consumeRecoveryCodeHash(got.TOTPRecoveryHashes, "AAAA-BBBB"); ok {
		t.Fatal("old recovery code survived rotation")
	}
}

// The enroll proof is checked against a snapshot; if the active factor is
// swapped, armed or disarmed before the mutation commits (a concurrent
// confirm, disable or admin reset), or the proving code got spent, the stale
// proof must not mint a seed against the new state (#1413).
func TestCheckEnrollProofCurrent(t *testing.T) {
	active := User{Username: "alice", TOTPEnabled: true, TOTPSecret: "JBSWY3DPEHPK3PXP", TOTPLastStep: 10}
	factorless := User{Username: "alice"}
	swapped := active
	swapped.TOTPSecret = "KRSXG5CTMVRXEZLU"
	spent := active
	spent.TOTPLastStep = 11
	for _, tc := range []struct {
		name              string
		snapshot, current User
		step              int64
		want              error
	}{
		{"first enroll unchanged", factorless, factorless, 0, nil},
		{"re-enroll unchanged", active, active, 11, nil},
		{"factor armed concurrently", factorless, active, 0, errTOTPFactorChanged},
		{"factor disarmed concurrently", active, factorless, 11, errTOTPFactorChanged},
		{"factor swapped concurrently", active, swapped, 11, errTOTPFactorChanged},
		{"proving code spent concurrently", active, spent, 11, errTOTPCodeConsumed},
	} {
		if got := checkEnrollProofCurrent(tc.snapshot, tc.current, tc.step); !errors.Is(got, tc.want) || (tc.want == nil && got != nil) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
