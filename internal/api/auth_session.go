package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type Session struct {
	Token     string
	Username  string
	Role      string
	CSRFToken string
	ExpiresAt time.Time
}

type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

var globalSessions = &SessionRegistry{
	sessions: make(map[string]Session),
}

func (r *SessionRegistry) NewSession(username, role string) Session {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, _ := generateRandomHex(32)
	csrf, _ := generateRandomHex(32)

	session := Session{
		Token:     token,
		Username:  username,
		Role:      role,
		CSRFToken: csrf,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	r.sessions[token] = session
	return session
}

func (r *SessionRegistry) Get(token string) (Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.sessions[token]
	if !ok {
		return Session{}, false
	}
	if time.Now().After(s.ExpiresAt) {
		r.mu.RUnlock()
		r.mu.Lock()
		delete(r.sessions, token)
		r.mu.Unlock()
		r.mu.RLock()
		return Session{}, false
	}
	return s, true
}

func (r *SessionRegistry) Delete(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, token)
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
	defer s.mu.Unlock()

	// Check if matching any stored user
	var matchedUser *User
	for _, u := range s.users {
		if u.Username == req.Username {
			matchedUser = &u
			break
		}
	}

	// Validate credentials
	valid := false
	role := "viewer"
	if matchedUser != nil {
		if err := bcrypt.CompareHashAndPassword([]byte(matchedUser.PasswordHash), []byte(req.Password)); err == nil {
			valid = true
			role = matchedUser.Role
		}
	} else if len(s.users) == 0 && s.settings.NaivePassword != "" && req.Username == "admin" {
		// Fallback for static auth token
		if req.Password == s.settings.NaivePassword {
			valid = true
			role = "admin"
		}
	}

	if !valid {
		writeError(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	session := globalSessions.NewSession(req.Username, role)

	http.SetCookie(w, &http.Cookie{
		Name:     "veil_session",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
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
		globalSessions.Delete(cookie.Value)
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
		if sess, ok := globalSessions.Get(cookie.Value); ok {
			writeJSON(w, map[string]any{
				"authenticated": true,
				"username":      sess.Username,
				"role":          sess.Role,
				"csrfToken":     sess.CSRFToken,
			})
			return
		}
	}

	writeJSON(w, map[string]any{
		"authenticated": false,
	})
}

func (s *managementState) handleUsersRoute(w http.ResponseWriter, r *http.Request) {
	// Only accessible if role is admin. Verified in middleware, but double check.
	cookie, err := r.Cookie("veil_session")
	var activeSession Session
	if err == nil {
		activeSession, _ = globalSessions.Get(cookie.Value)
	}
	if activeSession.Role != "admin" {
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
		// PUT and DELETE on /api/users/<username>
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
				return nil
			})

		} else if r.Method == http.MethodDelete {
			s.mu.Lock()
			exists := false
			for _, u := range s.users {
				if u.Username == username {
					exists = true
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

			if adminCount <= 1 {
				writeError(w, "cannot delete the last administrator", http.StatusBadRequest)
				return
			}

			_ = s.withMutation(func(mutation managementstate.Mutation) error {
				if mErr := mutation.DeleteUser(username); mErr != nil {
					writeError(w, mErr.Error(), http.StatusInternalServerError)
					return nil
				}
				w.WriteHeader(http.StatusNoContent)
				return nil
			})
		} else {
			methodNotAllowed(w, http.MethodPut, http.MethodDelete)
		}
	}
}
