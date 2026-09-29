package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/panel"
	"golang.org/x/crypto/bcrypt"
)

var errLoginCredentialsChanged = errors.New("login credentials changed during authentication")

type loginCredentialSnapshot struct {
	Username         string
	FoundUser        bool
	PasswordHash     string
	FallbackAllowed  bool
	FallbackPassword string
	// TOTPEnabled decides whether a verified password mints a real session or
	// only a pending_2fa challenge that must be completed at
	// POST /api/v1/auth/totp/verify (#1172).
	TOTPEnabled bool
}

func (s *managementState) snapshotLoginCredentials(username string) loginCredentialSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := loginCredentialSnapshot{Username: username}
	for _, user := range s.users {
		if user.Username == username {
			snapshot.FoundUser = true
			snapshot.PasswordHash = user.PasswordHash
			snapshot.TOTPEnabled = user.TOTPEnabled
			return snapshot
		}
	}
	// The NaivePassword fallback is a first-run escape hatch only: once the
	// instance has ever held users it must never re-arm, even if a rollback
	// or restore drops the user list back to zero (#1100).
	if len(s.users) == 0 && !s.usersProvisionedLocked() && username == "admin" && s.settings.NaivePassword != "" {
		snapshot.FallbackAllowed = true
		snapshot.FallbackPassword = s.settings.NaivePassword
	}
	return snapshot
}

func (snapshot loginCredentialSnapshot) passwordMatches(password string) bool {
	if snapshot.FoundUser {
		return bcrypt.CompareHashAndPassword([]byte(snapshot.PasswordHash), []byte(password)) == nil
	}
	// Unknown usernames still pay the bcrypt cost so the login response time
	// cannot reveal whether the account exists (#1064).
	_ = bcrypt.CompareHashAndPassword(dummyLoginPasswordHash, []byte(password))
	return snapshot.FallbackAllowed && constantTimePasswordEqual(password, snapshot.FallbackPassword)
}

// createSessionForLoginSnapshot mints the session for a verified login.
// secondFactor=true marks the session as having completed the second factor:
// only the pending_2fa verify path passes it, and only after the factor was
// actually validated (#1172).
func (s *managementState) createSessionForLoginSnapshot(snapshot loginCredentialSnapshot, r *http.Request, secondFactor bool) (Session, string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	role := "viewer"
	locale := panel.LocaleEnglish
	if snapshot.FoundUser {
		found := false
		for _, user := range s.users {
			if user.Username != snapshot.Username {
				continue
			}
			if user.PasswordHash != snapshot.PasswordHash {
				return Session{}, "", "", "", errLoginCredentialsChanged
			}
			role = user.Role
			locale = panel.NormalizeLocale(user.Locale)
			found = true
			break
		}
		if !found {
			return Session{}, "", "", "", errLoginCredentialsChanged
		}
	} else {
		if !snapshot.FallbackAllowed || len(s.users) != 0 || s.usersProvisionedLocked() ||
			snapshot.Username != "admin" || s.settings.NaivePassword != snapshot.FallbackPassword {
			return Session{}, "", "", "", errLoginCredentialsChanged
		}
		role = "admin"
	}

	// Fallback sessions are marked Bootstrap so the middleware exempts them
	// from user-match revocation only while the fallback precondition still
	// holds; ordinary sessions are never marked (#1112).
	session, err := s.sessionRegistry().Create(SessionCreateInput{
		Username:     snapshot.Username,
		Role:         role,
		UserAgent:    r.UserAgent(),
		RemoteAddr:   clientIP(r),
		Bootstrap:    !snapshot.FoundUser,
		SecondFactor: secondFactor,
	})
	return session, role, locale, s.settings.PanelAccess, err
}

// issuePendingSecondFactor converts a verified password into a pending_2fa
// challenge instead of a session. The pending cookie authorizes exactly one
// route — POST /api/v1/auth/totp/verify — for ~5 minutes. The login backoff
// budget is deliberately NOT cleared here: the factor-verify failures feed the
// same per-(client, username) throttle as password failures (#1172).
func (s *managementState) issuePendingSecondFactor(w http.ResponseWriter, r *http.Request, snapshot loginCredentialSnapshot) {
	token, expiresAt, err := s.pendingSecondFactors().Issue(snapshot.Username, snapshot.PasswordHash)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor:   snapshot.Username,
			Action:  "auth.login",
			Target:  "panel",
			Success: false,
			Error:   "second-factor challenge issuance failed",
		})
		writeError(w, "failed to start second-factor challenge", http.StatusInternalServerError)
		return
	}
	s.setPendingSecondFactorCookie(w, r, token, int(pendingSecondFactorTTL.Seconds()))
	s.recordRequestAudit(r, audit.Record{
		Actor:   snapshot.Username,
		Action:  "auth.login.pending_2fa",
		Target:  "panel",
		Success: true,
		Details: map[string]any{"methods": []string{"totp"}},
	})
	writeJSON(w, map[string]any{
		"success":              true,
		"secondFactorRequired": true,
		"secondFactorMethods":  []string{"totp"},
		"pendingExpiresAt":     expiresAt.Format(time.RFC3339),
	})
}

func (s *managementState) handleLoginWithRevalidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if s.rejectThrottledLogin(w, r, req.Username) {
		return
	}

	snapshot := s.snapshotLoginCredentials(req.Username)
	// A spray rotating across an IPv6 /64 (or across prefixes) can hold a
	// full bcrypt verify in flight per request; the process-wide username
	// budget slows such a spray to a crawl while the bcrypt semaphore caps
	// concurrent hash work globally (#1101).
	s.delayGlobalUsernameAttempt(req.Username)
	releaseBcrypt := acquireBcryptWork()
	if releaseBcrypt == nil {
		w.Header().Set("Retry-After", "1")
		s.recordRequestAudit(r, audit.Record{
			Actor:   req.Username,
			Action:  "auth.login.rate_limited",
			Target:  "panel",
			Success: false,
			Error:   "bcrypt work queue saturated",
		})
		writeError(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	matched := snapshot.passwordMatches(req.Password)
	releaseBcrypt()
	if !matched {
		s.recordInvalidLogin(w, r, req.Username)
		return
	}

	// A verified password is only HALF the credential when the account has
	// TOTP enabled: mint the short-lived pending_2fa challenge instead of a
	// session (#1172).
	if snapshot.FoundUser && snapshot.TOTPEnabled {
		s.issuePendingSecondFactor(w, r, snapshot)
		return
	}

	session, role, locale, _, err := s.createSessionForLoginSnapshot(snapshot, r, false)
	if errors.Is(err, errLoginCredentialsChanged) {
		s.recordRequestAudit(r, audit.Record{
			Actor:   req.Username,
			Action:  "auth.login",
			Target:  "panel",
			Success: false,
			Error:   "credentials changed during authentication",
		})
		writeError(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor:   req.Username,
			Role:    role,
			Action:  "auth.login",
			Target:  "panel",
			Success: false,
			Error:   "session persistence failed",
		})
		writeError(w, "failed to persist session", http.StatusInternalServerError)
		return
	}

	s.clearLoginFailures(loginThrottleKey(r, req.Username))
	s.recordRequestAudit(r, audit.Record{
		Actor:   req.Username,
		Role:    role,
		Action:  "auth.login",
		Target:  "panel",
		Success: true,
	})
	s.setSessionCookie(w, r, session.Token, 86400)
	writeJSON(w, map[string]any{
		"success":   true,
		"username":  req.Username,
		"role":      role,
		"locale":    locale,
		"csrfToken": session.CSRFToken,
	})
}
