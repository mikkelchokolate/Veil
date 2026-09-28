package api

import (
	"errors"
	"net/http"

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
}

func (s *managementState) snapshotLoginCredentials(username string) loginCredentialSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := loginCredentialSnapshot{Username: username}
	for _, user := range s.users {
		if user.Username == username {
			snapshot.FoundUser = true
			snapshot.PasswordHash = user.PasswordHash
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

func (s *managementState) createSessionForLoginSnapshot(snapshot loginCredentialSnapshot, r *http.Request) (Session, string, string, string, error) {
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
		Username:   snapshot.Username,
		Role:       role,
		UserAgent:  r.UserAgent(),
		RemoteAddr: clientIP(r),
		Bootstrap:  !snapshot.FoundUser,
	})
	return session, role, locale, s.settings.PanelAccess, err
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

	session, role, locale, _, err := s.createSessionForLoginSnapshot(snapshot, r)
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
