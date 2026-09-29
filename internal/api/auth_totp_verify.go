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
	pending, ok := s.pendingSecondFactors().Resolve(token)
	if !ok {
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
		return
	}
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		return
	}

	fail := func(auditErr string) {
		// Every failure counts twice: against the shared login backoff and
		// against the pending challenge's own attempt cap. Exhaustion destroys
		// the challenge so the caller must restart at the password stage.
		exhausted := s.pendingSecondFactors().RecordFailure(token)
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
		if !validateTOTPCode(s.loginBackoffTime(), current.TOTPSecret, req.Code) {
			fail("invalid verification code")
			return
		}
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
		return
	}

	s.pendingSecondFactors().Consume(token)
	s.setPendingSecondFactorCookie(w, r, "", -1)
	s.clearLoginFailures(loginThrottleKey(r, pending.Username))
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
	s.setSessionCookie(w, r, session.Token, 86400)
	writeJSON(w, map[string]any{
		"success":   true,
		"username":  pending.Username,
		"role":      role,
		"locale":    locale,
		"csrfToken": session.CSRFToken,
	})
}
