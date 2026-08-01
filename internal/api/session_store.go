package api

import (
	"bytes"
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

type sessionJournalRecord struct {
	Operation   string          `json:"operation"`
	TokenHash   string          `json:"tokenHash,omitempty"`
	Session     *storedSession  `json:"session,omitempty"`
	TokenHashes []string        `json:"tokenHashes,omitempty"`
	Sessions    []storedSession `json:"sessions,omitempty"`
}

const maxActiveSessions = 1024

type SessionRegistry struct {
	mu              sync.Mutex
	path            string
	sessions        map[string]storedSession
	rawCSRF         map[string]string
	now             func() time.Time
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
	persistInterval time.Duration
	storageErr      error
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
		persistInterval: 5 * time.Minute,
	}
	if err := registry.load(); err != nil {
		return nil, err
	}
	if evicted := registry.enforceSessionBoundLocked(); len(evicted) > 0 {
		if err := registry.saveLocked(); err != nil {
			return nil, err
		}
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

type evictedSession struct {
	hash   string
	record storedSession
	csrf   string
}

func (r *SessionRegistry) enforceSessionBoundLocked() []evictedSession {
	var evicted []evictedSession
	for len(r.sessions) > maxActiveSessions {
		oldestHash := ""
		var oldest storedSession
		for tokenHash, session := range r.sessions {
			if oldestHash == "" || session.CreatedAt.Before(oldest.CreatedAt) ||
				(session.CreatedAt.Equal(oldest.CreatedAt) && tokenHash < oldestHash) {
				oldestHash, oldest = tokenHash, session
			}
		}
		evicted = append(evicted, evictedSession{hash: oldestHash, record: oldest, csrf: r.rawCSRF[oldestHash]})
		delete(r.sessions, oldestHash)
		delete(r.rawCSRF, oldestHash)
	}
	return evicted
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
	evicted := r.enforceSessionBoundLocked()
	var persistErr error
	if len(evicted) > 0 {
		persistErr = r.saveLocked()
	} else {
		persistErr = r.persistUpsertLocked(record)
	}
	if persistErr != nil {
		delete(r.sessions, record.TokenHash)
		delete(r.rawCSRF, record.TokenHash)
		for _, previous := range evicted {
			r.sessions[previous.hash] = previous.record
			if previous.csrf != "" {
				r.rawCSRF[previous.hash] = previous.csrf
			}
		}
		return Session{}, persistErr
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
	if err := r.storageHealthyLocked(); err != nil {
		return Session{}, false
	}
	record, ok := r.sessions[tokenHash]
	if !ok {
		return Session{}, false
	}
	now := r.now().UTC()
	if now.Before(record.LastSeenAt) {
		now = record.LastSeenAt
	}
	if sessionExpired(record, now) {
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
		_ = r.persistDeleteLocked(tokenHash)
		return Session{}, false
	}
	interval := r.persistInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if now.Sub(record.LastSeenAt) >= interval {
		previous := record
		record.LastSeenAt = now
		record.IdleExpiresAt = minTime(now.Add(r.idleTimeout), record.ExpiresAt)
		r.sessions[tokenHash] = record
		if err := r.persistUpsertLocked(record); err != nil {
			r.sessions[tokenHash] = previous
			return Session{}, false
		}
	}
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
	if err := r.persistUpsertLocked(record); err != nil {
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

func (r *SessionRegistry) Delete(token string) error {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	record, existed := r.sessions[tokenHash]
	csrf := r.rawCSRF[tokenHash]
	delete(r.sessions, tokenHash)
	delete(r.rawCSRF, tokenHash)
	if !existed {
		return nil
	}
	if err := r.persistDeleteLocked(tokenHash); err != nil {
		r.sessions[tokenHash] = record
		if csrf != "" {
			r.rawCSRF[tokenHash] = csrf
		}
		return err
	}
	return nil
}

func (r *SessionRegistry) List(currentToken string) []SessionInfo {
	currentHash := hashSessionSecret(currentToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	list := make([]SessionInfo, 0, len(r.sessions))
	for tokenHash, session := range r.sessions {
		if sessionExpired(session, now) {
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			_ = r.persistDeleteLocked(tokenHash)
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
			csrf := r.rawCSRF[tokenHash]
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			if err := r.persistDeleteLocked(tokenHash); err != nil {
				r.sessions[tokenHash] = session
				if csrf != "" {
					r.rawCSRF[tokenHash] = csrf
				}
				return false
			}
			return true
		}
	}
	return false
}

func (r *SessionRegistry) DeleteByUsername(username string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var hashes []string
	for tokenHash, session := range r.sessions {
		if session.Username == username {
			hashes = append(hashes, tokenHash)
		}
	}
	if err := r.deleteManyLocked(hashes); err != nil {
		return 0, err
	}
	return len(hashes), nil
}

func (r *SessionRegistry) DeleteAllExcept(currentToken string) (int, error) {
	currentHash := hashSessionSecret(currentToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	var hashes []string
	for tokenHash := range r.sessions {
		if tokenHash != currentHash {
			hashes = append(hashes, tokenHash)
		}
	}
	if err := r.deleteManyLocked(hashes); err != nil {
		return 0, err
	}
	return len(hashes), nil
}

func (r *SessionRegistry) deleteManyLocked(hashes []string) error {
	removed := make(map[string]evictedSession, len(hashes))
	for _, tokenHash := range hashes {
		removed[tokenHash] = evictedSession{record: r.sessions[tokenHash], csrf: r.rawCSRF[tokenHash]}
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
	}
	if len(hashes) == 0 || r.path == "" {
		return nil
	}
	if err := r.appendJournalLocked(sessionJournalRecord{Operation: "delete_many", TokenHashes: hashes}); err != nil {
		for tokenHash, previous := range removed {
			r.sessions[tokenHash] = previous.record
			if previous.csrf != "" {
				r.rawCSRF[tokenHash] = previous.csrf
			}
		}
		return err
	}
	return nil
}

func (r *SessionRegistry) load() error {
	if r.path == "" {
		return nil
	}
	body, err := os.ReadFile(r.path)
	if err == nil {
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return r.loadJournalLocked()
}

func (r *SessionRegistry) RevalidateToken(token string, restoredRoles map[string]string) (bool, error) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.sessions[tokenHash]
	if !ok {
		return false, nil
	}
	role, exists := restoredRoles[record.Username]
	if !exists {
		if err := r.persistDeleteLocked(tokenHash); err != nil {
			return false, err
		}
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
		return false, nil
	}
	if record.Role != role {
		previous := record
		record.Role = role
		r.sessions[tokenHash] = record
		if err := r.persistUpsertLocked(record); err != nil {
			r.sessions[tokenHash] = previous
			return false, err
		}
	}
	return true, nil
}

func (r *SessionRegistry) Healthy() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.storageHealthyLocked()
}

func (r *SessionRegistry) journalPath() string {
	if r.path == "" {
		return ""
	}
	return r.path + ".journal"
}

func (r *SessionRegistry) storageHealthyLocked() error {
	if r.storageErr != nil {
		if err := r.compactJournalLocked(); err == nil {
			r.storageErr = nil
		} else {
			r.storageErr = err
			return err
		}
	}
	if r.path == "" {
		return nil
	}
	info, err := os.Stat(r.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("session snapshot is not a regular file")
	}
	return nil
}

func (r *SessionRegistry) persistUpsertLocked(record storedSession) error {
	if r.path == "" {
		return nil
	}
	if _, err := os.Stat(r.path); errors.Is(err, os.ErrNotExist) {
		return r.saveLocked()
	} else if err != nil {
		return err
	}
	copy := record
	return r.appendJournalLocked(sessionJournalRecord{Operation: "upsert", TokenHash: record.TokenHash, Session: &copy})
}

func (r *SessionRegistry) persistDeleteLocked(tokenHash string) error {
	if r.path == "" {
		return nil
	}
	if err := r.storageHealthyLocked(); err != nil {
		return err
	}
	return r.appendJournalLocked(sessionJournalRecord{Operation: "delete", TokenHash: tokenHash})
}

func (r *SessionRegistry) appendJournalLocked(record sessionJournalRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(r.journalPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := syncSessionDirectory(r.path); err != nil {
		return err
	}
	if info, err := os.Stat(r.journalPath()); err == nil && info.Size() > 1024*1024 {
		if compactErr := r.compactJournalLocked(); compactErr != nil {
			r.storageErr = compactErr
		}
	}
	return nil
}

func (r *SessionRegistry) compactJournalLocked() error {
	record := sessionJournalRecord{Operation: "replace_all", Sessions: make([]storedSession, 0, len(r.sessions))}
	for _, session := range r.sessions {
		record.Sessions = append(record.Sessions, session)
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return writeSessionFile(r.journalPath(), append(body, '\n'))
}

func (r *SessionRegistry) loadJournalLocked() error {
	if r.path == "" {
		return nil
	}
	body, err := os.ReadFile(r.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	complete := len(body) == 0 || body[len(body)-1] == '\n'
	lines := bytes.Split(body, []byte{'\n'})
	for index, line := range lines {
		if len(line) == 0 {
			continue
		}
		var record sessionJournalRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if !complete && index == len(lines)-1 {
				break
			}
			return err
		}
		switch record.Operation {
		case "upsert":
			if record.Session == nil || record.TokenHash == "" || record.Session.TokenHash != record.TokenHash {
				return errors.New("invalid session journal upsert")
			}
			r.sessions[record.TokenHash] = *record.Session
		case "delete":
			delete(r.sessions, record.TokenHash)
			delete(r.rawCSRF, record.TokenHash)
		case "delete_many":
			for _, tokenHash := range record.TokenHashes {
				delete(r.sessions, tokenHash)
				delete(r.rawCSRF, tokenHash)
			}
		case "replace_all":
			r.sessions = make(map[string]storedSession, len(record.Sessions))
			for _, session := range record.Sessions {
				if session.TokenHash == "" {
					return errors.New("invalid session journal checkpoint")
				}
				r.sessions[session.TokenHash] = session
			}
		default:
			return errors.New("invalid session journal operation")
		}
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
	if err := writeSessionFile(r.path, body); err != nil {
		return err
	}
	if err := os.Remove(r.journalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncSessionDirectory(r.path)
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
	return syncSessionDirectory(path)
}

func syncSessionDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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
