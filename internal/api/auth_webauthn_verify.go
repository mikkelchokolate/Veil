package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// WebAuthn login completion (#1171). handleWebAuthnLoginBegin issues the
// assertion challenge for the pending_2fa record minted at the password
// stage; handleWebAuthnLoginFinish validates the authenticator response and
// completes the pending challenge, minting the real veil_session. Both mirror
// handleTOTPVerify's contract: same throttle family, same per-challenge
// attempt cap, same fail-closed revalidation if the account moved under the
// challenge.

// resolveWebAuthnPending resolves the pending_2fa cookie and re-reads the
// account under the state mutex. Any drift since the password stage —
// renamed/gone user, rotated password hash, or the last passkey removed —
// consumes the challenge and fails closed, the same contract
// handleTOTPVerify applies.
//
// The claim is held for the WHOLE begin request — not just the resolve —
// so a begin cannot overwrite the ceremony challenge a finish is about to
// consume. Every non-terminal exit in the begin handler must Release it.
func (s *managementState) resolveWebAuthnPending(w http.ResponseWriter, r *http.Request) (pendingSecondFactor, model.User, string, bool) {
	token := pendingSecondFactorToken(r)
	pending, claim := s.pendingSecondFactors().Claim(token)
	switch claim {
	case pendingClaimBusy:
		// The cookie stays valid — a concurrent request holds the lease and
		// may Release it; the losing submit must not strip the retry path.
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.webauthn.login",
			Target:  "panel",
			Success: false,
			Error:   "concurrent verification on the same challenge",
		})
		writeError(w, "verification already in progress", http.StatusTooManyRequests)
		return pendingSecondFactor{}, model.User{}, "", false
	case pendingClaimMissing:
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.webauthn.login",
			Target:  "panel",
			Success: false,
			Error:   "unknown or expired second-factor challenge",
		})
		writeError(w, "second-factor challenge expired; sign in again", http.StatusUnauthorized)
		return pendingSecondFactor{}, model.User{}, "", false
	}
	s.mu.Lock()
	current, found := s.findUserLocked(pending.Username)
	s.mu.Unlock()
	if !found || current.PasswordHash != pending.PasswordHash || len(current.Passkeys) == 0 {
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.webauthn.login",
			Target:  "panel",
			Success: false,
			Error:   "account changed during second-factor challenge",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return pendingSecondFactor{}, model.User{}, "", false
	}
	return pending, current, token, true
}

// handleWebAuthnLoginBegin serves POST /api/v1/auth/webauthn/begin. The
// pending cookie authorizes it; the assertion challenge is bound to that
// token in the ceremony store so finish cannot mix challenges across
// accounts. Begin is deliberately NOT attempt-counted: it verifies nothing —
// but it does honor an active backoff so a throttled client cannot keep
// re-arming ceremonies.
func (s *managementState) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	pending, user, token, ok := s.resolveWebAuthnPending(w, r)
	if !ok {
		return
	}
	throttleKey := "2fa|" + loginThrottleKey(r, pending.Username)
	if allowed, retryAfter := s.allowLoginUsername(throttleKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		s.pendingSecondFactors().Release(token)
		return
	}
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		s.pendingSecondFactors().Release(token)
		return
	}
	waUser := webAuthnUser{username: user.Username, passkeys: user.Passkeys}
	if len(waUser.WebAuthnCredentials()) == 0 {
		// Every stored credential failed to decode — the account cannot
		// complete this factor, so the pending record dies with it (fail
		// closed rather than surfacing a library error as a 500).
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.begin", Target: "panel",
			Success: false, Error: "no usable passkeys on account",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}
	wa, _, err := webAuthnForRequest(r)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.begin", Target: "panel",
			Success: false, Error: err.Error(),
		})
		writeError(w, "passkey login is unavailable", http.StatusInternalServerError)
		s.pendingSecondFactors().Release(token)
		return
	}
	assertion, sessionData, err := wa.BeginLogin(
		waUser,
		// User verification is preferred, not required: the password stage
		// already supplied the knowledge factor, so UP-only authenticators
		// stay admissible while a UV-capable passkey still raises assurance.
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.begin", Target: "panel",
			Success: false, Error: err.Error(),
		})
		writeError(w, "failed to start passkey login", http.StatusInternalServerError)
		s.pendingSecondFactors().Release(token)
		return
	}
	s.webAuthnChallenges().Put(token, pending.Username, *sessionData)
	s.pendingSecondFactors().Release(token)
	s.recordRequestAudit(r, audit.Record{
		Actor:   pending.Username,
		Action:  "auth.webauthn.begin",
		Target:  "panel",
		Success: true,
	})
	writeJSON(w, assertion)
}

// handleWebAuthnLoginFinish serves POST /api/v1/auth/webauthn/finish and is
// the WebAuthn twin of handleTOTPVerify: resolve pending -> throttle ->
// revalidate account -> consume the stored ceremony -> validate the
// assertion -> persist the advanced sign counter -> mint the session. Every
// failure path is the same denial the TOTP path emits; nothing about the
// ceremony internals (credential IDs, counter values, origins) is ever
// disclosed in the response.
func (s *managementState) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	// The body IS the assertion JSON emitted by the browser's
	// navigator.credentials.get — parsed by the library rather than the
	// API's typed decoders so the wire shape stays authenticator-driven.
	parsed, err := protocol.ParseCredentialRequestResponseBody(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	if err != nil {
		writeError(w, "invalid passkey response", http.StatusBadRequest)
		return
	}
	token := pendingSecondFactorToken(r)
	// Claim takes the in-flight lease atomically: two parallel finishes on
	// the same pending cookie can no longer both mint sessions — the same
	// guarantee handleTOTPVerify makes for codes (#1172).
	pending, claim := s.pendingSecondFactors().Claim(token)
	switch claim {
	case pendingClaimBusy:
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.webauthn.finish",
			Target:  "panel",
			Success: false,
			Error:   "concurrent verification on the same challenge",
		})
		writeError(w, "verification already in progress", http.StatusTooManyRequests)
		return
	case pendingClaimMissing:
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.webauthn.finish",
			Target:  "panel",
			Success: false,
			Error:   "unknown or expired second-factor challenge",
		})
		writeError(w, "second-factor challenge expired; sign in again", http.StatusUnauthorized)
		return
	}

	// Same throttling family as the password stage and the TOTP verify
	// endpoint: per-(client, username) budget plus exponential backoff.
	throttleKey := "2fa|" + loginThrottleKey(r, pending.Username)
	if allowed, retryAfter := s.allowLoginUsername(throttleKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.webauthn.finish",
			Target:  "panel",
			Success: false,
			Error:   "rate limited",
		})
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		s.pendingSecondFactors().Release(token)
		return
	}
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		s.pendingSecondFactors().Release(token)
		return
	}

	fail := func(auditErr string) {
		// Every failure counts twice — against the shared login backoff and
		// against the pending challenge's own attempt cap — exactly like the
		// TOTP path, so the two factor endpoints cannot be combined to
		// outlive the cap.
		exhausted := s.pendingSecondFactors().RecordFailure(token)
		if !exhausted {
			// Retryable failure: free the in-flight lease so the next
			// assertion on this challenge is not denied by our own claim.
			s.pendingSecondFactors().Release(token)
		}
		delay := s.recordLoginFailure(throttleKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.webauthn.finish",
			Target:  "panel",
			Success: false,
			Error:   auditErr,
		})
		if exhausted {
			s.setPendingSecondFactorCookie(w, r, "", -1)
			writeError(w, "too many attempts; sign in again", http.StatusUnauthorized)
			return
		}
		writeError(w, "invalid passkey assertion", http.StatusUnauthorized)
	}

	// Revalidate the account under the state mutex before touching the
	// ceremony: the password hash must be the one verified at the password
	// stage and the account must still hold passkeys.
	s.mu.Lock()
	current, found := s.findUserLocked(pending.Username)
	s.mu.Unlock()
	if !found || current.PasswordHash != pending.PasswordHash || len(current.Passkeys) == 0 {
		s.pendingSecondFactors().Consume(token)
		s.webAuthnChallenges().Drop(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.webauthn.finish",
			Target:  "panel",
			Success: false,
			Error:   "account changed during second-factor challenge",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}

	// The ceremony challenge is single-use regardless of outcome: a failed
	// finish must begin again rather than replay or grind one challenge.
	challenge, found := s.webAuthnChallenges().Take(token)
	if !found || challenge.Username != pending.Username {
		fail("unknown or expired passkey ceremony")
		return
	}

	wa, _, err := webAuthnForRequest(r)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.finish", Target: "panel",
			Success: false, Error: err.Error(),
		})
		writeError(w, "passkey login is unavailable", http.StatusInternalServerError)
		s.pendingSecondFactors().Release(token)
		return
	}
	credential, err := wa.ValidateLogin(
		webAuthnUser{username: current.Username, passkeys: current.Passkeys},
		challenge.Session,
		parsed,
	)
	if err != nil {
		fail("invalid assertion")
		return
	}

	credentialID := passkeyIDEncoding.EncodeToString(credential.ID)
	if credential.Authenticator.CloneWarning {
		// Sign-count regression means the private key exists in two places:
		// the credential is invalidated — deleted from the account — not
		// merely denied once, so a cloned authenticator cannot be retried
		// under a fresh challenge (#1171).
		invalidateErr := s.withMutation(func(mutation managementstate.Mutation) error {
			fresh, ok := s.findUserLocked(pending.Username)
			if !ok {
				return errUserNotFound
			}
			kept := make([]model.Passkey, 0, len(fresh.Passkeys))
			for _, existing := range fresh.Passkeys {
				if existing.ID != credentialID {
					kept = append(kept, existing)
				}
			}
			update := fresh
			update.Passkeys = kept
			_, mErr := mutation.SetUserPasskeys(pending.Username, update)
			return mErr
		})
		s.catchUpAfterPanelMutation()
		if invalidateErr != nil {
			// The assertion is still denied, but the cloned credential could
			// not be removed durably — surface that in the audit trail so a
			// silent persist failure does not masquerade as a clean delete.
			fail("cloned credential detected; invalidation failed to persist")
			return
		}
		fail("cloned credential invalidated")
		return
	}

	// Persist the advanced sign counter BEFORE minting the session: a crash
	// between session creation and write-back would leave the counter stale,
	// and the next legitimate assertion would look like a clone.
	err = s.withMutation(func(mutation managementstate.Mutation) error {
		fresh, ok := s.findUserLocked(pending.Username)
		if !ok || fresh.PasswordHash != pending.PasswordHash {
			return errPasskeyVerifyFailed
		}
		update := fresh
		update.Passkeys = append([]model.Passkey(nil), fresh.Passkeys...)
		matched := false
		for i := range update.Passkeys {
			if update.Passkeys[i].ID == credentialID {
				passkeyApplyAssertion(&update.Passkeys[i], credential)
				matched = true
				break
			}
		}
		if !matched {
			return errPasskeyVerifyFailed
		}
		_, mErr := mutation.SetUserPasskeys(pending.Username, update)
		return mErr
	})
	if errors.Is(err, errPasskeyVerifyFailed) {
		// The credential disappeared (or the account rotated) between
		// validation and write-back: fail closed like any account drift.
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.finish", Target: "panel",
			Success: false, Error: "credential changed during assertion",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.finish", Target: "panel",
			Success: false, Error: "sign counter persistence failed",
		})
		writeError(w, "failed to persist passkey state", http.StatusInternalServerError)
		// Transient server failure — release the claim so a retry on the
		// same challenge is not wedged until the TTL.
		s.pendingSecondFactors().Release(token)
		return
	}
	s.catchUpAfterPanelMutation()

	// Same revalidation contract as the TOTP path: the snapshot must still
	// describe the live user row or the session mint fails closed (#1172).
	session, role, locale, _, err := s.createSessionForLoginSnapshot(loginCredentialSnapshot{
		Username:     pending.Username,
		FoundUser:    true,
		PasswordHash: pending.PasswordHash,
	}, r, true)
	if errors.Is(err, errLoginCredentialsChanged) {
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.finish", Target: "panel",
			Success: false, Error: "credentials changed during authentication",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: pending.Username, Action: "auth.webauthn.finish", Target: "panel",
			Success: false, Error: "session persistence failed",
		})
		writeError(w, "failed to persist session", http.StatusInternalServerError)
		s.pendingSecondFactors().Release(token)
		return
	}

	s.pendingSecondFactors().Consume(token)
	s.setPendingSecondFactorCookie(w, r, "", -1)
	// Failures were recorded against BOTH the password-stage key and the
	// 2fa-prefixed verify key — clear each or residue verify failures would
	// keep throttling future logins.
	s.clearLoginFailures(loginThrottleKey(r, pending.Username))
	s.clearLoginFailures(throttleKey)
	s.recordRequestAudit(r, audit.Record{
		Actor:   pending.Username,
		Role:    role,
		Action:  "auth.webauthn.finish",
		Target:  "panel",
		Success: true,
		Details: map[string]any{"credentialId": credentialID},
	})
	s.recordRequestAudit(r, audit.Record{
		Actor:   pending.Username,
		Role:    role,
		Action:  "auth.login",
		Target:  "panel",
		Success: true,
		Details: map[string]any{"secondFactor": "webauthn"},
	})
	s.setSessionCookie(w, r, session.Token, 86400)
	writeJSON(w, map[string]any{
		"success":      true,
		"username":     pending.Username,
		"role":         role,
		"locale":       locale,
		"csrfToken":    session.CSRFToken,
		"secondFactor": true,
	})
}
