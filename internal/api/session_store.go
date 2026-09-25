package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrSessionNotFound = errors.New("session not found")

func validateSessionIdentity(username, role, userAgent, remoteAddr string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 128 || !utf8.ValidString(username) {
		return errors.New("invalid session username")
	}
	if role != "admin" && role != "viewer" {
		return errors.New("invalid session role")
	}
	if len(userAgent) > 1024 || !utf8.ValidString(userAgent) || strings.ContainsAny(userAgent, "\r\n\x00") {
		return errors.New("invalid session user agent")
	}
	if len(remoteAddr) > 255 || !utf8.ValidString(remoteAddr) {
		return errors.New("invalid session remote address")
	}
	if remoteAddr != "" {
		host := remoteAddr
		if splitHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
			host = splitHost
		}
		if net.ParseIP(strings.Trim(host, "[]")) == nil {
			return errors.New("invalid session remote address")
		}
	}
	return nil
}

func validateStoredSession(session storedSession) error {
	if err := validateSessionIdentity(session.Username, session.Role, session.UserAgent, session.RemoteAddr); err != nil {
		return err
	}
	for _, hash := range []string{session.TokenHash, session.CSRFHash} {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("invalid session hash")
		}
	}
	if session.ID == "" || len(session.ID) > 64 || session.CreatedAt.IsZero() || session.LastSeenAt.Before(session.CreatedAt) ||
		session.IdleExpiresAt.Before(session.LastSeenAt) || session.ExpiresAt.Before(session.IdleExpiresAt) {
		return errors.New("invalid session timestamps")
	}
	return nil
}

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

// sessionRevocationIntent is a durable "revoke every session for this user
// created at or before Through" marker. It is journaled before a credential
// mutation commits so a crash between the mutation commit and the delete_many
// record cannot leave old-credential sessions valid past restart (#1059).
type sessionRevocationIntent struct {
	Username string    `json:"username"`
	Through  time.Time `json:"through"`
}

type sessionJournalRecord struct {
	Operation   string                    `json:"operation"`
	TokenHash   string                    `json:"tokenHash,omitempty"`
	Session     *storedSession            `json:"session,omitempty"`
	TokenHashes []string                  `json:"tokenHashes,omitempty"`
	Sessions    []storedSession           `json:"sessions,omitempty"`
	Username    string                    `json:"username,omitempty"`
	Through     time.Time                 `json:"through,omitempty"`
	Revocations []sessionRevocationIntent `json:"revocations,omitempty"`
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
	// pendingRevocations holds journaled revoke_username intents that have
	// not yet been fulfilled or cancelled. The list is rebuilt by journal
	// replay at load and serialized into replace_all checkpoints so an
	// intent outstanding across a compaction still applies (#1059).
	pendingRevocations []sessionRevocationIntent
}

var globalSessions = mustNewSessionRegistry("")

func persistedSessionStorePath(statePath string) string {
	statePath = strings.TrimSpace(statePath)
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), "sessions.json")
}

// InvalidatePersistedSessions removes the on-disk session snapshot and journal
// next to state.json. CLI restore uses this so pre-restore cookies cannot
// authenticate after the documented stop/restore/start recovery path.
func InvalidatePersistedSessions(statePath string) error {
	sessionPath := persistedSessionStorePath(statePath)
	if sessionPath == "" {
		return nil
	}
	var first error
	for _, path := range []string{sessionPath, sessionPath + ".journal"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if first == nil {
				first = err
			}
		}
	}
	return first
}

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
	if err := validateSessionIdentity(input.Username, input.Role, input.UserAgent, input.RemoteAddr); err != nil {
		return Session{}, err
	}
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

func (r *SessionRegistry) NewSession(username, role string) (Session, error) {
	return r.Create(SessionCreateInput{Username: username, Role: role})
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
			return publicSession(previous, token, r.rawCSRF[tokenHash]), true
		}
	}
	return publicSession(record, token, r.rawCSRF[tokenHash]), true
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

func (r *SessionRegistry) DeleteByID(id string) error {
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
				return err
			}
			return nil
		}
	}
	return ErrSessionNotFound
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

// MarkUsernameRevocationPending journals a durable intent to revoke every
// session for username created at or before the returned intent's Through
// bound. Callers must mark the intent BEFORE committing the credential
// mutation it pairs with: a crash between the mutation commit and the
// delete_many record would otherwise resurrect the old-credential sessions
// at the next load (#1059). The intent is fulfilled by a later
// delete/delete_many covering those sessions, or cancelled with
// CancelUsernameRevocation when the mutation rolls back.
func (r *SessionRegistry) MarkUsernameRevocationPending(username string) (sessionRevocationIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	intent := sessionRevocationIntent{Username: username, Through: r.now().UTC()}
	// Record the intent in memory BEFORE journaling it: a large journal makes
	// appendJournalLocked compact to a replace_all checkpoint that serializes
	// pendingRevocations, and an intent tracked only by the just-written line
	// would silently drop out of the checkpoint.
	r.pendingRevocations = append(r.pendingRevocations, intent)
	if r.path != "" {
		if err := r.appendJournalLocked(sessionJournalRecord{
			Operation: "revoke_username", Username: username, Through: intent.Through,
		}); err != nil {
			// Keep the pending intent: the caller aborts its mutation on this
			// error, and a stale intent only costs an extra re-login.
			return sessionRevocationIntent{}, err
		}
	}
	return intent, nil
}

// CancelUsernameRevocation retracts the outstanding revocation intents for
// the intent's username, used when the credential mutation they guard rolls
// back cleanly. A failed cancel only leaves a stale intent that forces an
// extra re-login after restart — safe, so callers may ignore the error.
func (r *SessionRegistry) CancelUsernameRevocation(intent sessionRevocationIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path != "" {
		if err := r.appendJournalLocked(sessionJournalRecord{
			Operation: "revoke_cancel", Username: intent.Username,
		}); err != nil {
			return err
		}
	}
	r.pendingRevocations = dropPendingRevocations(r.pendingRevocations, intent.Username)
	return nil
}

func dropPendingRevocations(intents []sessionRevocationIntent, username string) []sessionRevocationIntent {
	kept := intents[:0]
	for _, intent := range intents {
		if intent.Username != username {
			kept = append(kept, intent)
		}
	}
	return kept
}

// applyPendingRevocationsLocked enforces every outstanding revocation intent
// against the loaded session map: sessions created at or before the intent's
// Through bound are deleted. Intents are positional — sessions created after
// Through (for example a post-change re-login whose upsert was journaled
// after the intent) survive. Applied intents are consumed and dropped.
func (r *SessionRegistry) applyPendingRevocationsLocked() {
	for _, intent := range r.pendingRevocations {
		for tokenHash, session := range r.sessions {
			if session.Username == intent.Username && !session.CreatedAt.After(intent.Through) {
				delete(r.sessions, tokenHash)
				delete(r.rawCSRF, tokenHash)
			}
		}
	}
	r.pendingRevocations = nil
}

func (r *SessionRegistry) load() error {
	if r.path == "" {
		return nil
	}
	snapshotLoaded := false
	body, err := os.ReadFile(r.path)
	if err == nil {
		var file sessionStoreFile
		if err := json.Unmarshal(body, &file); err != nil {
			return err
		}
		if file.Version != 1 {
			return fmt.Errorf("unsupported session snapshot version %d", file.Version)
		}
		now := r.now().UTC()
		for _, session := range file.Sessions {
			if sessionExpired(session, now) {
				continue
			}
			if err := validateStoredSession(session); err != nil {
				return err
			}
			if session.ID == "" {
				session.ID = session.TokenHash[:minInt(16, len(session.TokenHash))]
			}
			r.sessions[session.TokenHash] = session
		}
		snapshotLoaded = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := r.loadJournalLocked(); err != nil {
		if snapshotLoaded {
			_ = r.quarantineJournalLocked()
			return nil
		}
		return err
	}
	return nil
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

func (r *SessionRegistry) quarantineJournalLocked() error {
	path := r.journalPath()
	if path == "" {
		return nil
	}
	suffix, err := generateRandomHex(8)
	if err != nil {
		return os.Remove(path)
	}
	quarantinePath := path + ".corrupt-" + suffix
	if err := os.Rename(path, quarantinePath); err != nil {
		return os.Remove(path)
	}
	return syncSessionDirectory(r.path)
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
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
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
	// Carry outstanding revocation intents into the checkpoint: an intent
	// whose mutation committed but whose delete_many never journaled must
	// still fire after the compaction rewrote the journal (#1059).
	record.Revocations = append(record.Revocations, r.pendingRevocations...)
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
			if record.Session == nil || record.TokenHash == "" || record.Session.TokenHash != record.TokenHash || validateStoredSession(*record.Session) != nil {
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
				if validateStoredSession(session) != nil {
					return errors.New("invalid session journal checkpoint")
				}
				r.sessions[session.TokenHash] = session
			}
			// The checkpoint is authoritative for outstanding intents: it
			// carries every intent pending when it was written (#1059).
			r.pendingRevocations = nil
			for _, intent := range record.Revocations {
				if intent.Username == "" || intent.Through.IsZero() {
					return errors.New("invalid session journal revocation intent")
				}
				r.pendingRevocations = append(r.pendingRevocations, intent)
			}
		case "revoke_username":
			if record.Username == "" || record.Through.IsZero() {
				return errors.New("invalid session journal revocation intent")
			}
			r.pendingRevocations = append(r.pendingRevocations, sessionRevocationIntent{
				Username: record.Username, Through: record.Through,
			})
		case "revoke_cancel":
			if record.Username == "" {
				return errors.New("invalid session journal revocation cancel")
			}
			r.pendingRevocations = dropPendingRevocations(r.pendingRevocations, record.Username)
		default:
			return errors.New("invalid session journal operation")
		}
	}
	// Replay is complete: enforce every intent that was never cancelled or
	// fulfilled — this is where a crash between the credential mutation
	// commit and the delete_many record is settled (#1059).
	r.applyPendingRevocationsLocked()
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
