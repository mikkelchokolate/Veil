package api

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

// pendingSecondFactorTTL bounds the window between password verification and
// second-factor completion. It is deliberately short: the pending cookie is a
// credential-grade artifact and must die fast so a stolen pending token cannot
// be stockpiled or replayed later (#1172).
const pendingSecondFactorTTL = 5 * time.Minute

// pendingSecondFactorMaxAttempts hard-caps code guesses against a single
// pending challenge: past this the challenge is destroyed and the caller must
// re-authenticate with a password. The per-(client, username) rate limiter and
// the shared login backoff still apply on top, so this bound is the last —
// always-enforced — line against TOTP brute force.
const pendingSecondFactorMaxAttempts = 5

// maxPendingSecondFactors bounds the pending-challenge map. A login spray that
// always passes the password stage could otherwise mint one pending record per
// attempt; evicting the stalest entries keeps memory flat and never fails the
// current request.
const maxPendingSecondFactors = 4096

// pendingSecondFactorCookie carries the pending-challenge token. It is NOT a
// session: nothing but POST /api/v1/auth/totp/verify accepts it.
const pendingSecondFactorCookie = "veil_pending_2fa"

// pendingSecondFactor is the server-side record behind a pending challenge.
// The shared pending_2fa machinery is factor-agnostic on purpose: a later
// WebAuthn flow issues the same kind of record and resolves it through the
// same verify endpoint family (issue #1172 builds the plumbing; WebAuthn
// reuses it).
type pendingSecondFactor struct {
	Username string
	// PasswordHash snapshots the credential as verified at the password
	// stage. If the user's hash changes before the factor completes, the
	// pending challenge must fail closed — the same revalidation contract
	// createSessionForLoginSnapshot applies to a same-request change.
	PasswordHash string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Attempts     int
	// InFlight marks a challenge currently being verified: a second
	// concurrent POST with the same pending cookie must never let two
	// requests both validate one code and mint two sessions (#1172).
	InFlight bool
}

type pendingSecondFactorStore struct {
	mu   sync.Mutex
	now  func() time.Time
	ttl  time.Duration
	byID map[string]pendingSecondFactor
}

func newPendingSecondFactorStore(now func() time.Time) *pendingSecondFactorStore {
	if now == nil {
		now = time.Now
	}
	return &pendingSecondFactorStore{
		now:  now,
		ttl:  pendingSecondFactorTTL,
		byID: make(map[string]pendingSecondFactor),
	}
}

var errPendingSecondFactorIssuance = errors.New("pending second-factor issuance failed")

// Issue mints a fresh pending challenge for username after a verified
// password. It returns the raw bearer token (placed in the pending cookie)
// and the challenge expiry.
func (s *pendingSecondFactorStore) Issue(username, passwordHash string) (token string, expiresAt time.Time, err error) {
	if username == "" {
		return "", time.Time{}, errPendingSecondFactorIssuance
	}
	token, err = generateRandomHex(32)
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	// Expired entries die on sight; the map is then bounded so unauthenticated
	// pressure cannot grow it without limit.
	for id, pending := range s.byID {
		if !pending.ExpiresAt.After(now) {
			delete(s.byID, id)
		}
	}
	for len(s.byID) >= maxPendingSecondFactors {
		oldestID, oldest := "", time.Time{}
		for id, pending := range s.byID {
			if oldestID == "" || pending.CreatedAt.Before(oldest) {
				oldestID, oldest = id, pending.CreatedAt
			}
		}
		delete(s.byID, oldestID)
	}
	expiresAt = now.Add(s.ttl)
	s.byID[hashSessionSecret(token)] = pendingSecondFactor{
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
	}
	return token, expiresAt, nil
}

// Resolve looks the pending challenge up and enforces its deadline. An
// expired or unknown token resolves to !ok — the caller fails closed with a
// 401 telling the client to restart the password stage.
func (s *pendingSecondFactorStore) Resolve(token string) (pendingSecondFactor, bool) {
	if token == "" {
		return pendingSecondFactor{}, false
	}
	id := hashSessionSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.byID[id]
	if !ok {
		return pendingSecondFactor{}, false
	}
	if !pending.ExpiresAt.After(s.now().UTC()) {
		delete(s.byID, id)
		return pendingSecondFactor{}, false
	}
	return pending, true
}

// Claim resolves the challenge AND atomically takes an in-flight lease on it:
// only one request can verify a given pending cookie at a time, so two
// parallel POSTs cannot race one factor code into two sessions. A claimed
// challenge must be completed by Consume (success/exhaustion) or Release
// (retryable failure); a claim abandoned mid-request stays closed until the
// TTL — the fail-closed side.
func (s *pendingSecondFactorStore) Claim(token string) (pendingSecondFactor, bool) {
	if token == "" {
		return pendingSecondFactor{}, false
	}
	id := hashSessionSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.byID[id]
	if !ok || pending.InFlight {
		return pendingSecondFactor{}, false
	}
	if !pending.ExpiresAt.After(s.now().UTC()) {
		delete(s.byID, id)
		return pendingSecondFactor{}, false
	}
	pending.InFlight = true
	s.byID[id] = pending
	return pending, true
}

// Release frees a claimed challenge after a retryable factor failure so the
// caller can submit another code. Unknown/consumed tokens are a no-op.
func (s *pendingSecondFactorStore) Release(token string) {
	if token == "" {
		return
	}
	id := hashSessionSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.byID[id]
	if !ok {
		return
	}
	pending.InFlight = false
	s.byID[id] = pending
}

// RecordFailure counts a failed factor attempt against the challenge and
// reports whether the challenge is now exhausted (destroyed). The caller maps
// exhausted=true onto "pending expired/invalid → restart login".
func (s *pendingSecondFactorStore) RecordFailure(token string) (exhausted bool) {
	if token == "" {
		return true
	}
	id := hashSessionSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.byID[id]
	if !ok {
		return true
	}
	pending.Attempts++
	if pending.Attempts >= pendingSecondFactorMaxAttempts {
		delete(s.byID, id)
		return true
	}
	s.byID[id] = pending
	return false
}

// Consume drops the challenge after a successful factor verification (or when
// the caller must invalidate it because the user record changed underneath).
func (s *pendingSecondFactorStore) Consume(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.byID, hashSessionSecret(token))
	s.mu.Unlock()
}

// pendingSecondFactors lazily binds the shared pending_2fa store onto the
// management state. now delegates to loginBackoffTime so the single auth-time
// test clock steers pending expiry exactly like login backoff.
func (s *managementState) pendingSecondFactors() *pendingSecondFactorStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending2FA == nil {
		s.pending2FA = newPendingSecondFactorStore(s.loginBackoffTime)
	}
	return s.pending2FA
}

// setPendingSecondFactorCookie writes the pending-challenge cookie with the
// same Path/Secure/SameSite discipline as the session cookie. MaxAge tracks
// the challenge TTL; a non-positive maxAge clears the cookie.
func (s *managementState) setPendingSecondFactorCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	path, secure := s.panelCookieAttrs(r)
	http.SetCookie(w, &http.Cookie{
		Name:     pendingSecondFactorCookie,
		Value:    token,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func pendingSecondFactorToken(r *http.Request) string {
	cookie, err := r.Cookie(pendingSecondFactorCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}
