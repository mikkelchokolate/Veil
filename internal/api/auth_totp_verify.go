package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

// handleTOTPVerify completes the pending_2fa login stage. The pending cookie
// authorizes ONLY this route; a valid factor mints the real veil_session.
// Exactly one of {code, recoveryCode} is required. The pending stage shares
// the login throttling machinery (per-(client,username) budget + exponential
// backoff), hard-caps attempts per challenge, and fails closed on any account
// change since the password was verified (#1172).
func (s *managementState) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req struct {
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if req.Code == "" && req.RecoveryCode == "" {
		writeError(w, "code or recoveryCode is required", http.StatusBadRequest)
		return
	}
	token := pendingSecondFactorToken(r)
	// Claim takes the in-flight lease atomically with the resolve: two
	// parallel POSTs on the same pending cookie can no longer both validate
	// one code into two sessions (#1172). A busy challenge keeps its cookie:
	// the in-flight request may still Release it after a wrong code, and the
	// losing submit must not strip the retry path (#1172 review).
	pending, claim := s.pendingSecondFactors().Claim(token)
	switch claim {
	case pendingClaimBusy:
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   "concurrent verification on the same challenge",
		})
		w.Header().Set("Retry-After", "1")
		writeError(w, "verification already in progress", http.StatusTooManyRequests)
		return
	case pendingClaimMissing:
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   "unknown or expired second-factor challenge",
		})
		writeError(w, "second-factor challenge expired; sign in again", http.StatusUnauthorized)
		return
	}

	// Same throttling family as the password stage: per-(client, username)
	// budget plus exponential backoff, so the 10^6 TOTP space cannot be
	// ground online and recovery codes cannot be sprayed.
	throttleKey := "2fa|" + loginThrottleKey(r, pending.Username)
	if allowed, retryAfter := s.allowLoginUsername(throttleKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.totp.verify",
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
		// Every failure counts twice: against the shared login backoff and
		// against the pending challenge's own attempt cap. Exhaustion destroys
		// the challenge so the caller must restart at the password stage.
		exhausted := s.pendingSecondFactors().RecordFailure(token)
		if !exhausted {
			// Retryable failure: free the in-flight lease so the next code
			// submission on this challenge is not denied by our own claim.
			s.pendingSecondFactors().Release(token)
		}
		delay := s.recordLoginFailure(throttleKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   auditErr,
		})
		if exhausted {
			s.setPendingSecondFactorCookie(w, r, "", -1)
			writeError(w, "too many attempts; sign in again", http.StatusUnauthorized)
			return
		}
		writeError(w, "invalid verification code", http.StatusUnauthorized)
	}

	// Revalidate the account under the state mutex: the password hash must be
	// the one verified at the password stage and the factor must still be
	// armed. Any drift consumes the challenge and restarts login.
	s.mu.Lock()
	current, found := s.findUserLocked(pending.Username)
	s.mu.Unlock()
	if !found || !current.TOTPEnabled || current.TOTPSecret == "" || current.PasswordHash != pending.PasswordHash {
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   "account changed during second-factor challenge",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}

	method := ""
	if req.Code != "" {
		matchedStep, valid := validateTOTPCode(s.loginBackoffTime(), current.TOTPSecret, req.Code)
		if !valid {
			fail("invalid verification code")
			return
		}
		// RFC 6238 §5.2 (#1220): an accepted code must never verify twice.
		// Persist the matched timestep as the account's anti-replay watermark
		// BEFORE the session exists — exactly like recovery-code consumption
		// below, a crash between session mint and write-back would otherwise
		// leave the code replayable on a fresh challenge.
		err := s.withMutation(func(mutation managementstate.Mutation) error {
			fresh, ok := s.findUserLocked(pending.Username)
			if !ok || !fresh.TOTPEnabled || fresh.TOTPSecret == "" || fresh.PasswordHash != pending.PasswordHash {
				return errTOTPVerifyFailed
			}
			if matchedStep <= fresh.TOTPLastStep {
				return errTOTPCodeConsumed
			}
			update := fresh
			update.TOTPLastStep = matchedStep
			_, mErr := mutation.SetUserTOTP(pending.Username, update)
			return mErr
		})
		if errors.Is(err, errTOTPCodeConsumed) {
			fail("verification code already used")
			return
		}
		if errors.Is(err, errTOTPVerifyFailed) {
			fail("invalid verification code")
			return
		}
		if err != nil {
			s.recordRequestAudit(r, audit.Record{
				Actor:   pending.Username,
				Action:  "auth.totp.verify",
				Target:  "panel",
				Success: false,
				Error:   "replay watermark persistence failed",
			})
			writeError(w, "failed to persist verification", http.StatusInternalServerError)
			// Transient persist failure — release the claim so a retry on
			// the same challenge is not wedged until the TTL.
			s.pendingSecondFactors().Release(token)
			return
		}
		s.catchUpAfterPanelMutation()
		method = "totp"
	} else {
		// Recovery codes are single-use AND the consumption must be durable
		// before the session exists — otherwise a crash between minting the
		// session and saving the spent code would make the code replayable.
		err := s.withMutation(func(mutation managementstate.Mutation) error {
			fresh, ok := s.findUserLocked(pending.Username)
			if !ok || !fresh.TOTPEnabled || fresh.PasswordHash != pending.PasswordHash {
				return errTOTPVerifyFailed
			}
			remaining, consumed := consumeRecoveryCodeHash(fresh.TOTPRecoveryHashes, req.RecoveryCode)
			if !consumed {
				return errTOTPVerifyFailed
			}
			update := fresh
			update.TOTPRecoveryHashes = remaining
			_, mErr := mutation.SetUserTOTP(pending.Username, update)
			return mErr
		})
		if errors.Is(err, errTOTPVerifyFailed) {
			fail("invalid recovery code")
			return
		}
		if err != nil {
			s.recordRequestAudit(r, audit.Record{
				Actor:   pending.Username,
				Action:  "auth.totp.verify",
				Target:  "panel",
				Success: false,
				Error:   "recovery code consumption failed to persist",
			})
			writeError(w, "failed to consume recovery code", http.StatusInternalServerError)
			// Transient persist failure — release the claim so a retry on
			// the same challenge is not wedged until the TTL.
			s.pendingSecondFactors().Release(token)
			return
		}
		s.catchUpAfterPanelMutation()
		method = "recovery"
	}

	// The password-hash check inside createSessionForLoginSnapshot is the same
	// revalidation the single-stage login performs — held against the pending
	// snapshot so a mid-flow credential change still fails closed (#1172).
	session, role, locale, _, err := s.createSessionForLoginSnapshot(loginCredentialSnapshot{
		Username:     pending.Username,
		FoundUser:    true,
		PasswordHash: pending.PasswordHash,
	}, r, true)
	if errors.Is(err, errLoginCredentialsChanged) {
		s.pendingSecondFactors().Consume(token)
		s.setPendingSecondFactorCookie(w, r, "", -1)
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   "credentials changed during authentication",
		})
		writeError(w, "account changed; sign in again", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor:   pending.Username,
			Action:  "auth.totp.verify",
			Target:  "panel",
			Success: false,
			Error:   "session persistence failed",
		})
		writeError(w, "failed to persist session", http.StatusInternalServerError)
		// Transient server failure — release the claim so a retry on the
		// same challenge is not wedged until the TTL.
		s.pendingSecondFactors().Release(token)
		return
	}

	s.pendingSecondFactors().Consume(token)
	s.setPendingSecondFactorCookie(w, r, "", -1)
	// Failures were recorded against BOTH the password-stage key and the
	// 2fa-prefixed verify key — a successful factor clears each, otherwise a
	// residue of verify-stage failures would keep throttling future logins.
	s.clearLoginFailures(loginThrottleKey(r, pending.Username))
	s.clearLoginFailures(throttleKey)
	s.recordRequestAudit(r, audit.Record{
		Actor:   pending.Username,
		Role:    role,
		Action:  "auth.totp.verify",
		Target:  "panel",
		Success: true,
		Details: map[string]any{"method": method},
	})
	s.recordRequestAudit(r, audit.Record{
		Actor:   pending.Username,
		Role:    role,
		Action:  "auth.login",
		Target:  "panel",
		Success: true,
		Details: map[string]any{"secondFactor": method},
	})
	// The body carries the fresh session's CSRF token — session-minting
	// responses are secret-grade for the idempotency store (#1221).
	markIdempotencySecretResponse(w, "session:"+pending.Username, 1)
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
