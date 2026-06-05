package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"golang.org/x/crypto/bcrypt"
)

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
	if foundUser {
		if err := bcrypt.CompareHashAndPassword([]byte(matchedUser.PasswordHash), []byte(req.Password)); err == nil {
			valid = true
			role = matchedUser.Role
		}
	} else if userCount == 0 && fallbackPassword != "" && req.Username == "admin" {
		if req.Password == fallbackPassword {
			valid = true
			role = "admin"
		}
	}

	if !valid {
		writeError(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	session, err := s.sessionRegistry().Create(SessionCreateInput{
		Username:   req.Username,
		Role:       role,
		UserAgent:  r.UserAgent(),
		RemoteAddr: clientIP(r),
	})
	if err != nil {
		writeError(w, "failed to persist session", http.StatusInternalServerError)
		return
	}

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
		"csrfToken": session.CSRFToken,
	})
}

func (s *managementState) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	cookie, err := r.Cookie("veil_session")
	if err == nil {
		s.sessionRegistry().Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "veil_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
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
				"csrfToken":     csrf,
			})
			return
		}
	}

	writeJSON(w, map[string]any{
		"authenticated": false,
	})
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
			writeNotFound(w)
			return
		}
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
		}
		var list []UserResponse
		for _, u := range s.users {
			list = append(list, UserResponse{Username: u.Username, Role: u.Role})
		}
		writeJSON(w, list)

	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Username == "" || req.Password == "" || (req.Role != "admin" && req.Role != "viewer") {
			writeError(w, "username, password, and valid role (admin/viewer) are required", http.StatusBadRequest)
			return
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
		}

		_ = s.withMutation(func(mutation managementstate.Mutation) error {
			created, mErr := mutation.CreateUser(newUser)
			if mErr != nil {
				writeError(w, mErr.Error(), http.StatusConflict)
				return nil
			}
			writeJSONStatus(w, http.StatusCreated, map[string]any{
				"username": created.Username,
				"role":     created.Role,
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
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Role != "admin" && req.Role != "viewer" {
			writeError(w, "valid role (admin/viewer) is required", http.StatusBadRequest)
			return
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
		}

		_ = s.withMutation(func(mutation managementstate.Mutation) error {
			updated, mErr := mutation.UpdateUser(username, update)
			if mErr != nil {
				writeError(w, mErr.Error(), http.StatusInternalServerError)
				return nil
			}
			writeJSON(w, map[string]any{
				"username": updated.Username,
				"role":     updated.Role,
			})
			_, _ = s.sessionRegistry().DeleteByUsername(username)
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
				writeError(w, mErr.Error(), http.StatusInternalServerError)
				return nil
			}
			_, _ = s.sessionRegistry().DeleteByUsername(username)
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
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	return r.RemoteAddr
}
