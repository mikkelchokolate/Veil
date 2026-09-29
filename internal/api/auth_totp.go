package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

var errTOTPVerifyFailed = errors.New("invalid verification code")

// errEnrollingSessionGone means the session that ran the TOTP confirm is no
// longer in the registry — the factor must not be armed behind it, so the
// confirm rolls back and the operator re-enrolls from a fresh session.
var errEnrollingSessionGone = errors.New("enrolling session expired")

// sessionUserForTOTP resolves the cookie-session identity these endpoints are
// scoped to. Self-service TOTP management requires a live browser session
// bound to a real user row: static API tokens and the dev-anonymous identity
// have no account to enroll (mirrors handleAuthLocale's session-mismatch
// guard).
func (s *managementState) sessionUserForTOTP(w http.ResponseWriter, r *http.Request) (Session, model.User, bool) {
	cookie, err := r.Cookie("veil_session")
	if err != nil {
		writeError(w, "an authenticated user session is required", http.StatusUnauthorized)
		return Session{}, model.User{}, false
	}
	session, ok := s.sessionRegistry().Get(cookie.Value)
	if !ok {
		writeError(w, "an authenticated user session is required", http.StatusUnauthorized)
		return Session{}, model.User{}, false
	}
	if actor, _ := r.Context().Value(contextKeyUsername).(string); actor != "" && actor != session.Username {
		writeError(w, "session user mismatch", http.StatusForbidden)
		return Session{}, model.User{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Username == session.Username {
			return session, user, true
		}
	}
	writeNotFound(w)
	return Session{}, model.User{}, false
}

// findUserLocked returns a copy of the current user row. Callers hold s.mu
// (directly or inside withMutation).
func (s *managementState) findUserLocked(username string) (model.User, bool) {
	for _, user := range s.users {
		if user.Username == username {
			return user, true
		}
	}
	return model.User{}, false
}

// sessionMeetsFactorRequirement reports whether an existing session may still
// be used: once its owner has TOTP enabled the session must carry the
// second-factor mark — minted by the verify step or upgraded at enrollment
// confirmation. Sessions minted before enrollment are retired on sight, which
// is also what makes "privilege changes revoke sessions" hold for 2FA (#1172).
func (s *managementState) sessionMeetsFactorRequirement(sess Session) bool {
	if sess.SecondFactor {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Username == sess.Username {
			return !user.TOTPEnabled
		}
	}
	return true
}

// handleMyTOTP serves GET (status) and DELETE (disable) on
// /api/v1/users/me/totp for the cookie session's own account.
func (s *managementState) handleMyTOTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleMyTOTPStatus(w, r)
	case http.MethodDelete:
		s.handleMyTOTPDisable(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *managementState) handleMyTOTPStatus(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.sessionUserForTOTP(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{
		"enabled":                user.TOTPEnabled,
		"pendingEnrollment":      user.TOTPPendingSecret != "",
		"recoveryCodesRemaining": len(user.TOTPRecoveryHashes),
	})
}

func (s *managementState) handleMyTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	_, user, ok := s.sessionUserForTOTP(w, r)
	if !ok {
		return
	}
	secret, uri, err := generateTOTPSecret(user.Username)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.enroll", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		writeError(w, "failed to generate TOTP secret", http.StatusInternalServerError)
		return
	}
	err = s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(user.Username)
		if !found {
			return errUserNotFound
		}
		// Re-enrollment only replaces the PENDING secret: the active factor
		// stays live until the new secret proves itself at confirm.
		update := current
		update.TOTPPendingSecret = secret
		_, mErr := mutation.SetUserTOTP(user.Username, update)
		return mErr
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.enroll", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		if errors.Is(err, errUserNotFound) {
			writeNotFound(w)
			return
		}
		writeError(w, "failed to persist TOTP enrollment", http.StatusInternalServerError)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.totp.enroll", Target: user.Username, Success: true,
	})
	writeJSON(w, map[string]any{
		"secret":     secret,
		"otpauthUri": uri,
		"issuer":     totpIssuer,
	})
}

func (s *managementState) handleMyTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	_, user, ok := s.sessionUserForTOTP(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if user.TOTPPendingSecret == "" {
		writeError(w, "no pending TOTP enrollment; start enrollment first", http.StatusBadRequest)
		return
	}
	// Confirm attempts reuse the login backoff family so an on-path observer
	// (or a stolen unlocked session) cannot grind through the 10^6 code space:
	// the budget is shared with password attempts per (client, username).
	throttleKey := "totp-confirm:" + loginThrottleKey(r, user.Username)
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if !validateTOTPCode(s.loginBackoffTime(), user.TOTPPendingSecret, req.Code) {
		delay := s.recordLoginFailure(throttleKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.confirm", Target: user.Username,
			Success: false, Error: "invalid verification code",
		})
		writeError(w, "invalid verification code", http.StatusBadRequest)
		return
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		writeError(w, "failed to generate recovery codes", http.StatusInternalServerError)
		return
	}
	err = s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(user.Username)
		if !found {
			return errUserNotFound
		}
		// The pending secret must still be the one just verified: a concurrent
		// re-enroll (or admin reset) swaps it, and activating the wrong secret
		// would brick the account's second factor.
		if current.TOTPPendingSecret == "" || current.TOTPPendingSecret != user.TOTPPendingSecret {
			return errTOTPVerifyFailed
		}
		// Journal the revocation intent BEFORE the factor commits so a crash
		// between commit and delete_many cannot leave pre-enrollment sessions
		// resurrectable at next load (#1059 pattern). The current session is
		// upgraded to second-factor-complete BEFORE the delete_many, and the
		// intent is cancelled afterwards so journal replay keeps it alive.
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(user.Username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		update := current
		update.TOTPEnabled = true
		update.TOTPSecret = current.TOTPPendingSecret
		update.TOTPPendingSecret = ""
		update.TOTPRecoveryHashes = hashes
		if _, mErr := mutation.SetUserTOTP(user.Username, update); mErr != nil {
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return mErr
		}
		marked, markErr := s.sessionRegistry().MarkSecondFactorPersisted(currentSessionToken(r))
		if markErr == nil && !marked {
			markErr = errEnrollingSessionGone
		}
		if markErr != nil {
			if _, restoreErr := mutation.SetUserTOTP(user.Username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, markErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			if errors.Is(markErr, errEnrollingSessionGone) {
				return markErr
			}
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, markErr)
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernameExceptPersisted(user.Username, currentSessionToken(r)); revokeErr != nil {
			if _, restoreErr := mutation.SetUserTOTP(user.Username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			// Restoring the user also unmarks the upgraded session: the
			// rollback returns the account to pre-confirm state, so its
			// sessions all stay valid until the flag check re-tightens.
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		if cancelErr := s.sessionRegistry().CancelUsernameRevocation(intent); cancelErr != nil {
			// A stale intent is safe (an extra re-login at worst) — the
			// mutation and revocation both committed.
			_ = cancelErr
		}
		return nil
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.confirm", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		switch {
		case errors.Is(err, errUserNotFound):
			writeNotFound(w)
		case errors.Is(err, errTOTPVerifyFailed):
			writeError(w, "pending TOTP enrollment changed; enroll again", http.StatusConflict)
		case errors.Is(err, errEnrollingSessionGone):
			writeError(w, errEnrollingSessionGone.Error()+"; sign in again", http.StatusUnauthorized)
		case errors.Is(err, errSessionRevocationPersistence):
			writeError(w, errSessionRevocationPersistence.Error(), http.StatusInternalServerError)
		default:
			writeError(w, "failed to persist TOTP confirmation", http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.totp.confirm", Target: user.Username,
		Success: true, Details: map[string]any{"recoveryCodes": recoveryCodeCount},
	})
	writeJSON(w, map[string]any{
		"enabled":       true,
		"recoveryCodes": codes,
	})
}

func (s *managementState) handleMyTOTPDisable(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.sessionUserForTOTP(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if !user.TOTPEnabled {
		writeError(w, "TOTP is not enabled", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, "authenticator code is required", http.StatusBadRequest)
		return
	}
	// Disabling a second factor is factor-grade, not credential-grade: a
	// stolen-but-verified session plus the account password must not be
	// enough to strip the factor. Require a live TOTP code — the operator
	// who lost the authenticator entirely goes through admin reset
	// (#1172). Throttled through the login backoff family so the disable
	// form cannot be turned into a code oracle.
	throttleKey := "totp-disable:" + loginThrottleKey(r, user.Username)
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if !validateTOTPCode(s.loginBackoffTime(), user.TOTPSecret, req.Code) {
		delay := s.recordLoginFailure(throttleKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.disable", Target: user.Username,
			Success: false, Error: "invalid authenticator code",
		})
		writeError(w, "invalid authenticator code", http.StatusBadRequest)
		return
	}
	err := s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(user.Username)
		if !found {
			return errUserNotFound
		}
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(user.Username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		update := current
		update.ClearTOTP()
		if _, mErr := mutation.SetUserTOTP(user.Username, update); mErr != nil {
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return mErr
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernameExceptPersisted(user.Username, currentSessionToken(r)); revokeErr != nil {
			if _, restoreErr := mutation.SetUserTOTP(user.Username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		_ = s.sessionRegistry().CancelUsernameRevocation(intent)
		return nil
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.totp.disable", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		switch {
		case errors.Is(err, errUserNotFound):
			writeNotFound(w)
		case errors.Is(err, errSessionRevocationPersistence):
			writeError(w, errSessionRevocationPersistence.Error(), http.StatusInternalServerError)
		default:
			writeError(w, "failed to persist TOTP disable", http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.totp.disable", Target: user.Username, Success: true,
	})
	writeJSON(w, map[string]any{"success": true})
}

// handleV1UserTOTPReset serves DELETE /api/v1/users/{username}/totp — the
// admin reset for a locked-out user. It clears the factor AND every session
// the target holds (a session must never survive the removal of the factor
// it bypassed).
func (s *managementState) handleV1UserTOTPReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	// The handler is registered on the /api/v1/users/ subtree; only
	// "{username}/totp" belongs to it.
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "totp" {
		writeNotFound(w)
		return
	}
	username := parts[0]

	err := s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(username)
		if !found {
			return errUserNotFound
		}
		if !current.HasUsableTOTPSecret() {
			return nil // already clean; keep reset idempotent
		}
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		update := current
		update.ClearTOTP()
		if _, mErr := mutation.SetUserTOTP(username, update); mErr != nil {
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return mErr
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernamePersisted(username); revokeErr != nil {
			if _, restoreErr := mutation.SetUserTOTP(username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		return nil
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Action: "user.totp.reset", Target: username, Success: false, Error: err.Error(),
		})
		switch {
		case errors.Is(err, errUserNotFound):
			writeNotFound(w)
		case errors.Is(err, errSessionRevocationPersistence):
			writeError(w, errSessionRevocationPersistence.Error(), http.StatusInternalServerError)
		default:
			writeError(w, "failed to persist TOTP reset", http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Action: "user.totp.reset", Target: username, Success: true,
	})
	w.WriteHeader(http.StatusNoContent)
}
