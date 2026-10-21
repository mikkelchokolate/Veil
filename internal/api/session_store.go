package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mikkelchokolate/Veil/internal/safefs"
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
	// Bootstrap marks a session created by the NaivePassword admin fallback:
	// no User row exists for it, so it is exempt from user-match revocation
	// ONLY while the fallback precondition still holds (#1112).
	Bootstrap bool
	// SecondFactor marks a session minted after the account's second factor
	// was satisfied (TOTP verify, or a session upgraded at TOTP enroll
	// confirmation — that confirmation IS a factor presentation). Sessions
	// lacking the flag are revoked on sight once the owner has TOTP enabled,
	// so a pre-enrollment cookie can never outlive the factor requirement
	// (#1172).
	SecondFactor bool
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
	SecondFactor  bool   `json:"secondFactor,omitempty"`
}

type SessionCreateInput struct {
	Username   string
	Role       string
	UserAgent  string
	RemoteAddr string
	// Bootstrap marks a session minted by the NaivePassword admin fallback
	// login (no backing user row). It is set only on the fallback path in
	// createSessionForLoginSnapshot (#1112).
	Bootstrap bool
	// SecondFactor marks a session minted after the account's second factor
	// was satisfied; see Session.SecondFactor (#1172).
	SecondFactor bool
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
	Bootstrap     bool      `json:"bootstrap,omitempty"`
	SecondFactor  bool      `json:"secondFactor,omitempty"`
}

type sessionStoreFile struct {
	Version  int             `json:"version"`
	Sessions []storedSession `json:"sessions"`
}

// sessionRevocationIntent is a durable "revoke every session for this user
// created at or before Through" marker. It is journaled before a credential
// mutation commits so a crash between the mutation commit and the delete_many
// record cannot leave old-credential sessions valid past restart (#1059).
//
// All marks a cross-process "revoke every session" intent journaled by the
// root CLI (`veil admin reset` / `rotate-key`, #1265); Username is empty then.
type sessionRevocationIntent struct {
	Username string    `json:"username"`
	Through  time.Time `json:"through"`
	All      bool      `json:"all,omitempty"`
}

// covers reports whether the intent revokes session.
func (intent sessionRevocationIntent) covers(session storedSession) bool {
	if !intent.All && session.Username != intent.Username {
		return false
	}
	return !session.CreatedAt.After(intent.Through)
}

func validRevocationIntent(intent sessionRevocationIntent) bool {
	if intent.Through.IsZero() {
		return false
	}
	if intent.All {
		return intent.Username == ""
	}
	return intent.Username != ""
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

// maxSessionsPerUser bounds the concurrent sessions one account may hold.
// Without a per-user quota a single low-privilege account can log in
// repeatedly until the global cap evicts every other user's — including every
// admin's — sessions (#1105).
const maxSessionsPerUser = 32

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
	if evicted := registry.enforceSessionBoundLocked("", ""); len(evicted) > 0 {
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

// enforceSessionBoundLocked trims the registry to both bounds. protectedHash
// is the session the caller just inserted (or is about to rely on): it is
// never evicted by this pass. newUsername scopes the per-user check to one
// account on the Create path; "" audits every account (registry load).
func (r *SessionRegistry) enforceSessionBoundLocked(protectedHash, newUsername string) []evictedSession {
	var evicted []evictedSession
	evictOne := func(hash string, record storedSession) {
		evicted = append(evicted, evictedSession{hash: hash, record: record, csrf: r.rawCSRF[hash]})
		delete(r.sessions, hash)
		delete(r.rawCSRF, hash)
	}
	oldestFirst := func(hashes []string) {
		sort.Slice(hashes, func(i, j int) bool {
			a, b := r.sessions[hashes[i]], r.sessions[hashes[j]]
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return hashes[i] < hashes[j]
		})
	}
	// pickOldest prefers non-admin sessions over admin ones at equal
	// priority so the global bound cannot shed administrators merely for
	// having older sessions; ties then fall to CreatedAt, then the hash for
	// deterministic behavior (#1105).
	pickOldest := func(candidates []string) (string, storedSession, bool) {
		best := ""
		var bestRecord storedSession
		for _, hash := range candidates {
			if hash == protectedHash {
				continue
			}
			record, ok := r.sessions[hash]
			if !ok {
				continue
			}
			if best == "" ||
				(bestRecord.Role == "admin" && record.Role != "admin") ||
				(bestRecord.Role == "admin") == (record.Role == "admin") &&
					(record.CreatedAt.Before(bestRecord.CreatedAt) ||
						(record.CreatedAt.Equal(bestRecord.CreatedAt) && hash < best)) {
				best, bestRecord = hash, record
			}
		}
		return best, bestRecord, best != ""
	}

	// Per-user bound: overflow always comes out of the offending account's
	// own oldest sessions, so one user's logins can never evict another
	// user's session (#1105).
	usernames := map[string]bool{}
	if newUsername != "" {
		usernames[newUsername] = true
	} else {
		for _, session := range r.sessions {
			usernames[session.Username] = true
		}
	}
	for username := range usernames {
		var owned []string
		for hash, session := range r.sessions {
			if session.Username == username {
				owned = append(owned, hash)
			}
		}
		overflow := len(owned) - maxSessionsPerUser
		if overflow <= 0 {
			continue
		}
		oldestFirst(owned)
		for _, hash := range owned {
			if overflow <= 0 {
				break
			}
			if hash == protectedHash {
				continue
			}
			evictOne(hash, r.sessions[hash])
			overflow--
		}
	}

	// Global bound: a last-resort memory bound only. When it bites, evict the
	// oldest session belonging to the account holding the MOST sessions —
	// the user generating the pressure absorbs the eviction rather than
	// whichever session happens to be globally oldest (#1105). pickOldest's
	// admin preference is the second line of defence: an attacker flooding
	// distinct usernames sheds non-admin sessions first.
	for len(r.sessions) > maxActiveSessions {
		counts := make(map[string]int, 64)
		for _, session := range r.sessions {
			counts[session.Username]++
		}
		maxCount := 0
		for _, count := range counts {
			if count > maxCount {
				maxCount = count
			}
		}
		var bucket []string
		for hash, session := range r.sessions {
			if counts[session.Username] == maxCount {
				bucket = append(bucket, hash)
			}
		}
		if hash, record, ok := pickOldest(bucket); ok {
			evictOne(hash, record)
			continue
		}
		var all []string
		for hash := range r.sessions {
			all = append(all, hash)
		}
		if hash, record, ok := pickOldest(all); ok {
			evictOne(hash, record)
			continue
		}
		break
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
		Bootstrap:     input.Bootstrap,
		SecondFactor:  input.SecondFactor,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[record.TokenHash] = record
	r.rawCSRF[record.TokenHash] = csrf
	evicted := r.enforceSessionBoundLocked(record.TokenHash, input.Username)
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

// Peek reports whether token still names a live, unexpired session without
// sliding its idle window or persisting anything. Long-lived streams use it
// to re-check their authorization (#1290): a background event stream is not
// user activity, so it must neither extend the idle timeout nor outlive it.
// Expired records are left for Get to reap.
func (r *SessionRegistry) Peek(token string) (Session, bool) {
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
		return Session{}, false
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
			SecondFactor:  session.SecondFactor,
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
		// The cancel names the exact intent it retracts (Through), so it can
		// never retract a different intent for the same user, such as one
		// the root CLI journaled concurrently (#1265).
		if err := r.appendJournalLocked(sessionJournalRecord{
			Operation: "revoke_cancel", Username: intent.Username, Through: intent.Through,
		}); err != nil {
			return err
		}
	}
	r.pendingRevocations = cancelPendingRevocation(r.pendingRevocations, intent.Username, intent.Through)
	return nil
}

// cancelPendingRevocation drops the per-user intents a revoke_cancel record
// retracts. A cancel with a Through bound (written since #1265) retracts only
// the intent with that exact bound; a legacy cancel without one retracts every
// per-user intent for the username. "All" intents are never cancelled.
func cancelPendingRevocation(intents []sessionRevocationIntent, username string, through time.Time) []sessionRevocationIntent {
	kept := intents[:0]
	for _, intent := range intents {
		if !intent.All && intent.Username == username && (through.IsZero() || intent.Through.Equal(through)) {
			continue
		}
		kept = append(kept, intent)
	}
	return kept
}

func dropPendingRevocations(intents []sessionRevocationIntent, username string) []sessionRevocationIntent {
	kept := intents[:0]
	for _, intent := range intents {
		if intent.All || intent.Username != username {
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
			if intent.covers(session) {
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
	body, err := readSessionStoreFile(r.path)
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
	// Lstat: a symlink planted at the snapshot path reports as itself (not a
	// regular file) instead of vouching for whatever it points at (#1284).
	info, err := os.Lstat(r.path)
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
	if _, err := os.Lstat(r.path); errors.Is(err, os.ErrNotExist) {
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
	// The session directory (/var/lib/veil) is writable by the unprivileged
	// service account, while root CLI paths (`veil admin set` →
	// DeleteByUsername) append here too. Resolve the journal through a pinned
	// directory handle with O_NOFOLLOW|O_NONBLOCK and fstat the descriptor, so
	// a planted symlink cannot turn this append into a root create/append of
	// an arbitrary file and a planted FIFO cannot hang the caller (#1284).
	parent, err := openSessionStoreDir(r.path)
	if err != nil {
		return err
	}
	defer parent.Close()
	journalName := filepath.Base(r.journalPath())
	file, err := parent.AppendFileAt(journalName, 0o600)
	if err != nil {
		return fmt.Errorf("open session journal: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("session journal %s is not a regular file", r.journalPath())
	}
	if err := matchSessionStoreOwner(parent.File(), file); err != nil {
		_ = file.Close()
		return fmt.Errorf("chown session journal: %w", err)
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	// Size from the descriptor we just wrote, not a fresh path lookup.
	info, statErr := file.Stat()
	if err := file.Close(); err != nil {
		return err
	}
	if err := parent.File().Sync(); err != nil {
		return err
	}
	if statErr == nil && info.Size() > 1024*1024 {
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
	body, err := readSessionStoreFile(r.journalPath())
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
				if !validRevocationIntent(intent) {
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
		case "revoke_all":
			// Journaled by the root CLI (`veil admin reset`/`rotate-key`):
			// every session created at or before Through is revoked, even
			// one a later upsert re-adds (#1265).
			intent := sessionRevocationIntent{Through: record.Through, All: true}
			if record.Username != "" || !validRevocationIntent(intent) {
				return errors.New("invalid session journal revocation intent")
			}
			r.pendingRevocations = append(r.pendingRevocations, intent)
		case "revoke_cancel":
			if record.Username == "" {
				return errors.New("invalid session journal revocation cancel")
			}
			r.pendingRevocations = cancelPendingRevocation(r.pendingRevocations, record.Username, record.Through)
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
	// Temp create, chmod and publish all go through one pinned directory
	// handle: the chmod is an fchmod on the O_EXCL-created descriptor, so a
	// temp name swapped for a symlink between write and chmod can no longer
	// redirect a root chmod onto an arbitrary file (#1284).
	parent, err := openSessionStoreDir(path)
	if err != nil {
		return err
	}
	defer parent.Close()
	tmp, tmpName, err := parent.CreateTempAt(".sessions-", 0o600)
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = parent.RemoveAt(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := matchSessionStoreOwner(parent.File(), tmp); err != nil {
		cleanup()
		return err
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
		_ = parent.RemoveAt(tmpName)
		return err
	}
	name := filepath.Base(path)
	if runtime.GOOS == "windows" {
		if err := parent.RemoveAt(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = parent.RemoveAt(tmpName)
			return err
		}
	}
	if err := parent.RenameAt(tmpName, name); err != nil {
		_ = parent.RemoveAt(tmpName)
		return err
	}
	return parent.File().Sync()
}

// maxSessionStoreFileBytes bounds a snapshot/journal read. The journal is
// compacted past 1 MiB and the snapshot is capped by the per-user session
// bound, so anything near this size is not a file Veil wrote.
const maxSessionStoreFileBytes = 64 << 20

// openSessionStoreDir creates (when missing) and pins the directory holding
// the session store. The directory itself is operator-configured (next to
// state.json), so a symlinked root is followed once and then pinned; every
// leaf below it is resolved relative to the handle without following.
func openSessionStoreDir(path string) (*safefs.Dir, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return safefs.OpenDirFollow(dir)
}

// readSessionStoreFile reads the session snapshot or journal without
// following a planted leaf symlink and without blocking on (or reading from)
// a FIFO/device: the leaf is opened O_NOFOLLOW|O_NONBLOCK relative to the
// pinned parent and must fstat as a regular file. A missing directory or leaf
// reports os.ErrNotExist like os.ReadFile did (#1284).
func readSessionStoreFile(path string) ([]byte, error) {
	parent, err := safefs.OpenDirFollow(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := parent.OpenFileAt(filepath.Base(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("open session store file %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("session store file %s is not a regular file", path)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxSessionStoreFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSessionStoreFileBytes {
		return nil, fmt.Errorf("session store file %s exceeds %d bytes", path, maxSessionStoreFileBytes)
	}
	return body, nil
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
		Bootstrap:     record.Bootstrap,
		SecondFactor:  record.SecondFactor,
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
