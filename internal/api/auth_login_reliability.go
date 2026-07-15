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
	if len(s.users) == 0 && username == "admin" && s.settings.NaivePassword != "" {
		snapshot.FallbackAllowed = true
		snapshot.FallbackPassword = s.settings.NaivePassword
	}
	return snapshot
}

func (snapshot loginCredentialSnapshot) passwordMatches(password string) bool {
	if snapshot.FoundUser {
		return bcrypt.CompareHashAndPassword([]byte(snapshot.PasswordHash), []byte(password)) == nil
	}
	return snapshot.FallbackAllowed && password == snapshot.FallbackPassword
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
		if !snapshot.FallbackAllowed || len(s.users) != 0 || snapshot.Username != "admin" || s.settings.NaivePassword != snapshot.FallbackPassword {
			return Session{}, "", "", "", errLoginCredentialsChanged
		}
		role = "admin"
	}

	session, err := s.sessionRegistry().Create(SessionCreateInput{
		Username:   snapshot.Username,
		Role:       role,
		UserAgent:  r.UserAgent(),
		RemoteAddr: clientIP(r),
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

	snapshot := s.snapshotLoginCredentials(req.Username)
	if !snapshot.passwordMatches(req.Password) {
		s.recordRequestAudit(r, audit.Record{
			Actor:   req.Username,
			Action:  "auth.login",
			Target:  "panel",
			Success: false,
			Error:   "invalid credentials",
		})
		writeError(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	session, role, locale, panelAccess, err := s.createSessionForLoginSnapshot(snapshot, r)
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

	s.recordRequestAudit(r, audit.Record{
		Actor:   req.Username,
		Role:    role,
		Action:  "auth.login",
		Target:  "panel",
		Success: true,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "veil_session",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || panelAccess == "caddy",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	writeJSON(w, map[string]any{
		"success":   true,
		"username":  req.Username,
		"role":      role,
		"locale":    locale,
		"csrfToken": session.CSRFToken,
	})
}
