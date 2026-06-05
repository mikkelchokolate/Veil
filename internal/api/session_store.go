package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	defaultSessionIdleTimeout     = 30 * time.Minute
	defaultSessionAbsoluteTimeout = 24 * time.Hour
)

type Session struct {
	ID            string
	Token         string
	Username      string
	Role          string
	CSRFToken     string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	IdleExpiresAt time.Time
	ExpiresAt     time.Time
	UserAgent     string
	RemoteAddr    string
}

type SessionInfo struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	CreatedAt     string `json:"createdAt"`
	LastSeenAt    string `json:"lastSeenAt"`
	IdleExpiresAt string `json:"idleExpiresAt"`
	ExpiresAt     string `json:"expiresAt"`
	UserAgent     string `json:"userAgent,omitempty"`
	RemoteAddr    string `json:"remoteAddr,omitempty"`
	Current       bool   `json:"current"`
}

type SessionCreateInput struct {
	Username   string
	Role       string
	UserAgent  string
	RemoteAddr string
}

type storedSession struct {
	ID            string    `json:"id"`
	TokenHash     string    `json:"tokenHash"`
	CSRFHash      string    `json:"csrfHash"`
	Username      string    `json:"username"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"createdAt"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
	IdleExpiresAt time.Time `json:"idleExpiresAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	UserAgent     string    `json:"userAgent,omitempty"`
	RemoteAddr    string    `json:"remoteAddr,omitempty"`
}

type sessionStoreFile struct {
	Version  int             `json:"version"`
	Sessions []storedSession `json:"sessions"`
}

type SessionRegistry struct {
	mu              sync.Mutex
	path            string
	sessions        map[string]storedSession
	rawCSRF         map[string]string
	now             func() time.Time
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
}

var globalSessions = mustNewSessionRegistry("")

func NewSessionRegistry(path string) (*SessionRegistry, error) {
	registry := &SessionRegistry{
		path:            path,
		sessions:        make(map[string]storedSession),
		rawCSRF:         make(map[string]string),
		now:             time.Now,
		idleTimeout:     defaultSessionIdleTimeout,
		absoluteTimeout: defaultSessionAbsoluteTimeout,
	}
	if err := registry.load(); err != nil {
		return nil, err
	}
	return registry, nil
}

func mustNewSessionRegistry(path string) *SessionRegistry {
	registry, err := NewSessionRegistry(path)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *SessionRegistry) Create(input SessionCreateInput) (Session, error) {
	token, err := generateRandomHex(32)
	if err != nil {
		return Session{}, err
	}
	csrf, err := generateRandomHex(32)
	if err != nil {
		return Session{}, err
	}
	now := r.now().UTC()
	expiresAt := now.Add(r.absoluteTimeout)
	record := storedSession{
		ID:            sessionID(token),
		TokenHash:     hashSessionSecret(token),
		CSRFHash:      hashSessionSecret(csrf),
		Username:      input.Username,
		Role:          input.Role,
		CreatedAt:     now,
		LastSeenAt:    now,
		IdleExpiresAt: minTime(now.Add(r.idleTimeout), expiresAt),
		ExpiresAt:     expiresAt,
		UserAgent:     input.UserAgent,
		RemoteAddr:    input.RemoteAddr,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[record.TokenHash] = record
	r.rawCSRF[record.TokenHash] = csrf
	if err := r.saveLocked(); err != nil {
		delete(r.sessions, record.TokenHash)
		delete(r.rawCSRF, record.TokenHash)
		return Session{}, err
	}
	return publicSession(record, token, csrf), nil
}

func (r *SessionRegistry) NewSession(username, role string) Session {
	session, _ := r.Create(SessionCreateInput{Username: username, Role: role})
	return session
}

func (r *SessionRegistry) Get(token string) (Session, bool) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.sessions[tokenHash]
	if !ok {
		return Session{}, false
	}
	now := r.now().UTC()
	if sessionExpired(record, now) {
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
		_ = r.saveLocked()
		return Session{}, false
	}
	record.LastSeenAt = now
	record.IdleExpiresAt = minTime(now.Add(r.idleTimeout), record.ExpiresAt)
	r.sessions[tokenHash] = record
	_ = r.saveLocked()
	return publicSession(record, token, r.rawCSRF[tokenHash]), true
}

func (r *SessionRegistry) EnsureCSRF(token string) (string, bool, error) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.sessions[tokenHash]
	if !ok || sessionExpired(record, r.now().UTC()) {
		return "", false, nil
	}
	if csrf := r.rawCSRF[tokenHash]; csrf != "" {
		return csrf, true, nil
	}
	csrf, err := generateRandomHex(32)
	if err != nil {
		return "", false, err
	}
	record.CSRFHash = hashSessionSecret(csrf)
	r.sessions[tokenHash] = record
	r.rawCSRF[tokenHash] = csrf
	if err := r.saveLocked(); err != nil {
		delete(r.rawCSRF, tokenHash)
		return "", false, err
	}
	return csrf, true, nil
}

func (r *SessionRegistry) ValidateCSRF(token, provided string) bool {
	if token == "" || provided == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.sessions[hashSessionSecret(token)]
	if !ok || sessionExpired(record, r.now().UTC()) {
		return false
	}
	got := hashSessionSecret(provided)
	return subtle.ConstantTimeCompare([]byte(got), []byte(record.CSRFHash)) == 1
}

func (r *SessionRegistry) Delete(token string) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, tokenHash)
	delete(r.rawCSRF, tokenHash)
	_ = r.saveLocked()
}

func (r *SessionRegistry) List(currentToken string) []SessionInfo {
	currentHash := hashSessionSecret(currentToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	list := make([]SessionInfo, 0, len(r.sessions))
	changed := false
	for tokenHash, session := range r.sessions {
		if sessionExpired(session, now) {
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			changed = true
			continue
		}
		list = append(list, SessionInfo{
			ID:            session.ID,
			Username:      session.Username,
			Role:          session.Role,
			CreatedAt:     session.CreatedAt.Format(time.RFC3339),
			LastSeenAt:    session.LastSeenAt.Format(time.RFC3339),
			IdleExpiresAt: session.IdleExpiresAt.Format(time.RFC3339),
			ExpiresAt:     session.ExpiresAt.Format(time.RFC3339),
			UserAgent:     session.UserAgent,
			RemoteAddr:    session.RemoteAddr,
			Current:       tokenHash == currentHash,
		})
	}
	if changed {
		_ = r.saveLocked()
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Current != list[j].Current {
			return list[i].Current
		}
		if list[i].Username != list[j].Username {
			return list[i].Username < list[j].Username
		}
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return list
}

func (r *SessionRegistry) DeleteByID(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for tokenHash, session := range r.sessions {
		if session.ID == id {
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			_ = r.saveLocked()
			return true
		}
	}
	return false
}

func (r *SessionRegistry) DeleteByUsername(username string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deleted := 0
	for tokenHash, session := range r.sessions {
		if session.Username == username {
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			deleted++
		}
	}
	if deleted == 0 {
		return 0, nil
	}
	return deleted, r.saveLocked()
}

func (r *SessionRegistry) DeleteAllExcept(currentToken string) (int, error) {
	currentHash := hashSessionSecret(currentToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	deleted := 0
	for tokenHash := range r.sessions {
		if tokenHash == currentHash {
			continue
		}
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
		deleted++
	}
	if deleted == 0 {
		return 0, nil
	}
	return deleted, r.saveLocked()
}

func (r *SessionRegistry) load() error {
	if r.path == "" {
		return nil
	}
	body, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file sessionStoreFile
	if err := json.Unmarshal(body, &file); err != nil {
		return err
	}
	now := r.now().UTC()
	for _, session := range file.Sessions {
		if session.TokenHash == "" || sessionExpired(session, now) {
			continue
		}
		if session.ID == "" {
			session.ID = session.TokenHash[:minInt(16, len(session.TokenHash))]
		}
		r.sessions[session.TokenHash] = session
	}
	return nil
}

func (r *SessionRegistry) saveLocked() error {
	if r.path == "" {
		return nil
	}
	file := sessionStoreFile{Version: 1, Sessions: make([]storedSession, 0, len(r.sessions))}
	for _, session := range r.sessions {
		file.Sessions = append(file.Sessions, session)
	}
	sort.Slice(file.Sessions, func(i, j int) bool {
		return file.Sessions[i].CreatedAt.Before(file.Sessions[j].CreatedAt)
	})
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeSessionFile(r.path, body)
}

func writeSessionFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sessions-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(body); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tmpPath)
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func publicSession(record storedSession, token, csrf string) Session {
	return Session{
		ID:            record.ID,
		Token:         token,
		Username:      record.Username,
		Role:          record.Role,
		CSRFToken:     csrf,
		CreatedAt:     record.CreatedAt,
		LastSeenAt:    record.LastSeenAt,
		IdleExpiresAt: record.IdleExpiresAt,
		ExpiresAt:     record.ExpiresAt,
		UserAgent:     record.UserAgent,
		RemoteAddr:    record.RemoteAddr,
	}
}

func sessionExpired(session storedSession, now time.Time) bool {
	return !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt)
}

func hashSessionSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func sessionID(token string) string {
	hash := hashSessionSecret(token)
	return hash[:16]
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
