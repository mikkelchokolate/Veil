package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/clientaddr"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/observability"
	"github.com/mikkelchokolate/Veil/internal/panel"
	"golang.org/x/crypto/bcrypt"
)

var errUserNotFound = errors.New("user not found")

func constantTimePasswordEqual(supplied, expected string) bool {
	suppliedDigest := sha256.Sum256([]byte(supplied))
	expectedDigest := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(suppliedDigest[:], expectedDigest[:]) == 1
}

func (s *managementState) sessionRegistry() *SessionRegistry {
	if s != nil && s.sessions != nil {
		return s.sessions
	}
	return globalSessions
}

func (s *managementState) handleLogin(w http.ResponseWriter, r *http.Request) {
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
	usernameKey := strings.ToLower(strings.TrimSpace(req.Username))
	if allowed, retryAfter := s.allowLoginUsername(usernameKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor: req.Username, Action: "auth.login.rate_limited", Target: "panel",
			Success: false, Error: "username rate limit",
		})
		writeError(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	if retryAfter := s.loginBackoffRemaining(usernameKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}

	s.mu.Lock()
	var matchedUser User
	foundUser := false
	for _, u := range s.users {
		if u.Username == req.Username {
			matchedUser = u
			foundUser = true
			break
		}
	}
	userCount := len(s.users)
	fallbackPassword := s.settings.NaivePassword
	panelAccess := s.settings.PanelAccess
	s.mu.Unlock()

	valid := false
	role := "viewer"
	locale := panel.LocaleEnglish
	if foundUser {
		if err := bcrypt.CompareHashAndPassword([]byte(matchedUser.PasswordHash), []byte(req.Password)); err == nil {
			valid = true
			role = matchedUser.Role
			locale = panel.NormalizeLocale(matchedUser.Locale)
		}
	} else if userCount == 0 && fallbackPassword != "" && req.Username == "admin" {
		if constantTimePasswordEqual(req.Password, fallbackPassword) {
			valid = true
			role = "admin"
		}
	}

	if !valid {
		retryAfter := s.recordLoginFailure(usernameKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
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

	s.clearLoginFailures(usernameKey)
	session, err := s.sessionRegistry().Create(SessionCreateInput{
		Username:   req.Username,
		Role:       role,
		UserAgent:  r.UserAgent(),
		RemoteAddr: clientIP(r),
	})
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

func (s *managementState) allowLoginUsername(normalizedUsername string) (bool, time.Duration) {
	s.mu.Lock()
	if s.loginUsernameLimiter == nil {
		s.loginUsernameLimiter = observability.NewRateLimiterEngine()
	}
	limiter := s.loginUsernameLimiter
	s.mu.Unlock()
	return limiter.Allow("login-username:"+normalizedUsername, 5.0/60.0, 3)
}

func (s *managementState) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	cookie, err := r.Cookie("veil_session")
	if err == nil {
		actor, role := "", ""
		if session, ok := s.sessionRegistry().Get(cookie.Value); ok {
			actor, role = session.Username, session.Role
		}
		if err := s.sessionRegistry().Delete(cookie.Value); err != nil {
			s.recordRequestAudit(r, audit.Record{Actor: actor, Role: role, Action: "auth.logout", Target: "panel", Success: false, Error: "session revocation persistence failed"})
			writeError(w, "failed to revoke session", http.StatusInternalServerError)
			return
		}
		s.recordRequestAudit(r, audit.Record{
			Actor:   actor,
			Role:    role,
			Action:  "auth.logout",
			Target:  "panel",
			Success: true,
		})
	}

	panelAccess := s.settings.PanelAccess
	http.SetCookie(w, &http.Cookie{
		Name:     "veil_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || panelAccess == "caddy",
		MaxAge:   -1,
	})

	writeJSON(w, map[string]any{"success": true})
}

func (s *managementState) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	cookie, err := r.Cookie("veil_session")
	if err == nil {
		if sess, ok := s.sessionRegistry().Get(cookie.Value); ok {
			csrf, csrfOK, csrfErr := s.sessionRegistry().EnsureCSRF(cookie.Value)
			if csrfErr != nil {
				writeError(w, "failed to refresh session", http.StatusInternalServerError)
				return
			}
			if !csrfOK {
				writeJSON(w, map[string]any{"authenticated": false})
				return
			}
			writeJSON(w, map[string]any{
				"authenticated": true,
				"username":      sess.Username,
				"role":          sess.Role,
				"locale":        s.userLocale(sess.Username),
				"csrfToken":     csrf,
			})
			return
		}
	}

	writeJSON(w, map[string]any{
		"authenticated": false,
	})
}

func (s *managementState) handleAuthLocale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	cookie, err := r.Cookie("veil_session")
	if err != nil {
		writeError(w, "an authenticated user session is required", http.StatusUnauthorized)
		return
	}
	session, ok := s.sessionRegistry().Get(cookie.Value)
	if !ok {
		writeError(w, "an authenticated user session is required", http.StatusUnauthorized)
		return
	}
	if actor, _ := r.Context().Value(contextKeyUsername).(string); actor != "" && actor != session.Username {
		writeError(w, "session user mismatch", http.StatusForbidden)
		return
	}
	var req struct {
		Locale string `json:"locale"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	locale, ok := panel.ParseLocale(req.Locale)
	if !ok || (locale != panel.LocaleEnglish && locale != panel.LocaleRussian) {
		writeError(w, "locale must be en or ru", http.StatusBadRequest)
		return
	}

	var updated model.User
	err = s.withMutation(func(mutation managementstate.Mutation) error {
		var current model.User
		found := false
		for _, user := range mutation.Users() {
			if user.Username == session.Username {
				current = user
				found = true
				break
			}
		}
		if !found {
			return errUserNotFound
		}
		current.Locale = locale
		var updateErr error
		updated, updateErr = mutation.UpdateUser(session.Username, current)
		return updateErr
	})
	if err != nil {
		if err == errUserNotFound {
			writeNotFound(w)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	panelAccess := s.settings.PanelAccess
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "veil_locale",
		Value:    updated.Locale,
		Path:     "/",
		Secure:   r.TLS != nil || panelAccess == "caddy",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   31536000,
	})
	s.recordRequestAudit(r, audit.Record{
		Actor:   session.Username,
		Role:    session.Role,
		Action:  "auth.locale.update",
		Target:  session.Username,
		Success: true,
		Details: map[string]any{"locale": updated.Locale},
	})
	writeJSON(w, map[string]string{"locale": updated.Locale})
}

func (s *managementState) userLocale(username string) string {
	return panel.NormalizeLocale(s.storedUserLocale(username))
}

func (s *managementState) storedUserLocale(username string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Username == username {
			return user.Locale
		}
	}
	return ""
}

func (s *managementState) handleAuthSessions(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.sessionRegistry().List(currentSessionToken(r)))
	case http.MethodDelete:
		var req struct {
			ID string `json:"id"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			writeError(w, "session id is required", http.StatusBadRequest)
			return
		}
		if !s.sessionRegistry().DeleteByID(req.ID) {
			s.recordRequestAudit(r, audit.Record{
				Action:  "auth.session.revoke",
				Target:  req.ID,
				Success: false,
				Error:   "session not found",
			})
			writeNotFound(w)
			return
		}
		s.recordRequestAudit(r, audit.Record{
			Action:  "auth.session.revoke",
			Target:  req.ID,
			Success: true,
		})
		writeJSON(w, map[string]any{"success": true})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *managementState) handleUsersRoute(w http.ResponseWriter, r *http.Request) {
	// Only accessible if role is admin. Verified in middleware, but double check.
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		type UserResponse struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			Locale   string `json:"locale"`
		}
		var list []UserResponse
		for _, u := range s.users {
			list = append(list, UserResponse{
				Username: u.Username,
				Role:     u.Role,
				Locale:   panel.NormalizeLocale(u.Locale),
			})
		}
		writeJSON(w, list)

	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
			Locale   string `json:"locale,omitempty"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Username == "" || req.Password == "" || (req.Role != "admin" && req.Role != "viewer") {
			writeError(w, "username, password, and valid role (admin/viewer) are required", http.StatusBadRequest)
			return
		}
		if req.Locale == "" {
			req.Locale = panel.LocaleEnglish
		} else {
			locale, ok := panel.ParseLocale(req.Locale)
			if !ok {
				writeError(w, "locale must be en or ru", http.StatusBadRequest)
				return
			}
			req.Locale = locale
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		newUser := model.User{
			Username:     req.Username,
			PasswordHash: string(hashed),
			Role:         req.Role,
			Locale:       req.Locale,
		}

		_ = s.withMutation(func(mutation managementstate.Mutation) error {
			created, mErr := mutation.CreateUser(newUser)
			if mErr != nil {
				s.recordRequestAudit(r, audit.Record{
					Action:  "user.create",
					Target:  req.Username,
					Success: false,
					Error:   mErr.Error(),
				})
				writeError(w, mErr.Error(), http.StatusConflict)
				return nil
			}
			s.recordRequestAudit(r, audit.Record{
				Action:  "user.create",
				Target:  created.Username,
				Success: true,
				Details: map[string]any{"role": created.Role},
			})
			writeJSONStatus(w, http.StatusCreated, map[string]any{
				"username": created.Username,
				"role":     created.Role,
				"locale":   created.Locale,
			})
			return nil
		})

	default:
		s.handleUserByNameRoute(w, r)
	}
}

func (s *managementState) handleUserByNameRoute(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) != 3 {
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	username := pathParts[2]

	if r.Method == http.MethodPut {
		var req struct {
			Password string `json:"password,omitempty"`
			Role     string `json:"role"`
			Locale   string `json:"locale,omitempty"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Role != "admin" && req.Role != "viewer" {
			writeError(w, "valid role (admin/viewer) is required", http.StatusBadRequest)
			return
		}
		if req.Locale != "" {
			locale, ok := panel.ParseLocale(req.Locale)
			if !ok {
				writeError(w, "locale must be en or ru", http.StatusBadRequest)
				return
			}
			req.Locale = locale
		}

		s.mu.Lock()
		var targetUser *User
		for _, u := range s.users {
			if u.Username == username {
				targetUser = &u
				break
			}
		}
		s.mu.Unlock()

		if targetUser == nil {
			writeNotFound(w)
			return
		}

		hash := targetUser.PasswordHash
		if req.Password != "" {
			hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
			if err != nil {
				writeError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hash = string(hashed)
		}

		update := model.User{
			Username:     username,
			PasswordHash: hash,
			Role:         req.Role,
			Locale:       req.Locale,
		}

		_ = s.withMutation(func(mutation managementstate.Mutation) error {
			updated, mErr := mutation.UpdateUser(username, update)
			if mErr != nil {
				s.recordRequestAudit(r, audit.Record{
					Action:  "user.update",
					Target:  username,
					Success: false,
					Error:   mErr.Error(),
				})
				writeError(w, mErr.Error(), http.StatusInternalServerError)
				return nil
			}
			writeJSON(w, map[string]any{
				"username": updated.Username,
				"role":     updated.Role,
				"locale":   panel.NormalizeLocale(updated.Locale),
			})
			_, _ = s.sessionRegistry().DeleteByUsername(username)
			s.recordRequestAudit(r, audit.Record{
				Action:  "user.update",
				Target:  username,
				Success: true,
				Details: map[string]any{
					"role":            updated.Role,
					"passwordChanged": req.Password != "",
				},
			})
			return nil
		})

	} else if r.Method == http.MethodDelete {
		s.mu.Lock()
		exists := false
		targetRole := ""
		for _, u := range s.users {
			if u.Username == username {
				exists = true
				targetRole = u.Role
				break
			}
		}
		s.mu.Unlock()

		if !exists {
			writeNotFound(w)
			return
		}

		// Prevent deleting the last admin
		s.mu.Lock()
		adminCount := 0
		for _, u := range s.users {
			if u.Role == "admin" {
				adminCount++
			}
		}
		s.mu.Unlock()

		if targetRole == "admin" && adminCount <= 1 {
			writeError(w, "cannot delete the last administrator", http.StatusBadRequest)
			return
		}

		_ = s.withMutation(func(mutation managementstate.Mutation) error {
			if mErr := mutation.DeleteUser(username); mErr != nil {
				s.recordRequestAudit(r, audit.Record{
					Action:  "user.delete",
					Target:  username,
					Success: false,
					Error:   mErr.Error(),
				})
				writeError(w, mErr.Error(), http.StatusInternalServerError)
				return nil
			}
			if _, dErr := s.sessionRegistry().DeleteUsernamePersisted(username); dErr != nil {
				s.recordRequestAudit(r, audit.Record{
					Action:  "user.delete",
					Target:  username,
					Success: false,
					Error:   dErr.Error(),
				})
				writeError(w, dErr.Error(), http.StatusInternalServerError)
				return dErr
			}
			s.recordRequestAudit(r, audit.Record{
				Action:  "user.delete",
				Target:  username,
				Success: true,
			})
			w.WriteHeader(http.StatusNoContent)
			return nil
		})
	} else {
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func requestHasAdminRole(state *managementState, r *http.Request) bool {
	if role, _ := r.Context().Value(contextKeyRole).(string); role == "admin" {
		return true
	}
	cookie, err := r.Cookie("veil_session")
	var activeSession Session
	if err == nil {
		activeSession, _ = state.sessionRegistry().Get(cookie.Value)
	}
	return activeSession.Role == "admin"
}

func currentSessionToken(r *http.Request) string {
	cookie, err := r.Cookie("veil_session")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func clientIP(r *http.Request) string {
	if address, ok := clientaddr.FromContext(r); ok {
		return address
	}
	address, err := (clientaddr.Resolver{}).Resolve(r)
	if err != nil {
		return r.RemoteAddr
	}
	return address
}
