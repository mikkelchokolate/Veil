package api

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	idempotencyTTL        = 24 * time.Hour
	maxIdempotencyEntries = 4096
	maxIdempotencyBody    = 1024 * 1024
)

type idempotencyEntry struct {
	fingerprint string
	createdAt   time.Time
	status      int
	header      http.Header
	body        []byte
	done        chan struct{}
	doneOnce    sync.Once
	completed   bool
}

func (e *idempotencyEntry) signal() { e.doneOnce.Do(func() { close(e.done) }) }

type idempotencyStore struct {
	mu      sync.Mutex
	entries map[string]*idempotencyEntry
	now     func() time.Time
	closed  bool
	db      *sql.DB
	owner   string
}

func newIdempotencyStore(databases ...*sql.DB) *idempotencyStore {
	store := &idempotencyStore{entries: make(map[string]*idempotencyEntry), now: time.Now}
	if len(databases) > 0 {
		store.db = databases[0]
	}
	store.owner = idempotencyOwnerID()
	return store
}

func (s *idempotencyStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		for key, entry := range s.entries {
			delete(s.entries, key)
			entry.signal()
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *idempotencyStore) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" || !idempotencyEligible(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !validIdempotencyKey(key) {
			writeError(w, "invalid Idempotency-Key", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
		if err != nil {
			writeError(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := idempotencyFingerprint(r, body)
		scope := idempotencyScope(r, key)
		if s.db != nil {
			s.serveDurable(w, r, next, key, scope, fingerprint)
			return
		}
		entry, owner, conflict, unavailable := s.acquire(scope, fingerprint)
		if conflict {
			writeError(w, "Idempotency-Key was already used with a different request", http.StatusConflict)
			return
		}
		if unavailable {
			writeError(w, "idempotency service unavailable", http.StatusServiceUnavailable)
			return
		}
		if !owner {
			select {
			case <-entry.done:
			case <-r.Context().Done():
				writeError(w, "request canceled while waiting for idempotent operation", http.StatusRequestTimeout)
				return
			}
			if !entry.completed {
				writeError(w, "idempotent operation did not complete", http.StatusServiceUnavailable)
				return
			}
			replayIdempotencyResponse(w, entry)
			return
		}

		capture := newBufferedResponse(w.Header())
		finished := false
		defer func() {
			if !finished {
				s.abort(scope, entry)
			}
		}()
		next.ServeHTTP(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= http.StatusOK && status < http.StatusBadRequest && capture.body.Len() <= maxIdempotencyBody {
			s.complete(scope, entry, status, capture.header, capture.body.Bytes())
		} else {
			s.abort(scope, entry)
		}
		finished = true
		copyHTTPHeader(w.Header(), capture.header)
		w.WriteHeader(status)
		_, _ = w.Write(capture.body.Bytes())
	})
}

func (s *idempotencyStore) acquire(scope, fingerprint string) (*idempotencyEntry, bool, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, false, true
	}
	now := s.now()
	for key, entry := range s.entries {
		if entry.completed && now.Sub(entry.createdAt) >= idempotencyTTL {
			delete(s.entries, key)
		}
	}
	if entry := s.entries[scope]; entry != nil {
		return entry, false, entry.fingerprint != fingerprint, false
	}
	if len(s.entries) >= maxIdempotencyEntries {
		for key, entry := range s.entries {
			if entry.completed {
				delete(s.entries, key)
				break
			}
		}
	}
	if len(s.entries) >= maxIdempotencyEntries {
		return nil, false, false, true
	}
	entry := &idempotencyEntry{fingerprint: fingerprint, createdAt: now, done: make(chan struct{})}
	s.entries[scope] = entry
	return entry, true, false, false
}

func (s *idempotencyStore) complete(scope string, entry *idempotencyEntry, status int, header http.Header, body []byte) {
	s.mu.Lock()
	if !s.closed && s.entries[scope] == entry {
		entry.status = status
		entry.header = header.Clone()
		entry.header.Del("X-Request-ID")
		entry.body = append([]byte(nil), body...)
		entry.completed = true
	}
	entry.signal()
	s.mu.Unlock()
}

func (s *idempotencyStore) abort(scope string, entry *idempotencyEntry) {
	s.mu.Lock()
	if s.entries[scope] == entry {
		delete(s.entries, scope)
	}
	entry.signal()
	s.mu.Unlock()
}

func idempotencyEligible(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.URL.Path {
	case "/api/auth/login", "/api/auth/logout":
		return false
	default:
		return true
	}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 || !utf8.ValidString(key) {
		return false
	}
	for _, char := range key {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func idempotencyScope(r *http.Request, key string) string {
	actor := actorFromRequest(r)
	if actor == "" {
		actor = clientIP(r)
	}
	return actor + "\x00" + r.Method + "\x00" + r.URL.EscapedPath() + "\x00" + key
}

func idempotencyFingerprint(r *http.Request, body []byte) string {
	digest := sha256.New()
	_, _ = io.WriteString(digest, r.Method)
	_, _ = io.WriteString(digest, "\n"+r.URL.EscapedPath()+"?"+r.URL.RawQuery+"\n")
	_, _ = digest.Write(body)
	return hex.EncodeToString(digest.Sum(nil))
}

func replayIdempotencyResponse(w http.ResponseWriter, entry *idempotencyEntry) {
	copyHTTPHeader(w.Header(), entry.header)
	w.Header().Set("Idempotency-Replayed", "true")
	w.WriteHeader(entry.status)
	_, _ = w.Write(entry.body)
}

func copyHTTPHeader(destination, source http.Header) {
	for key, values := range source {
		if strings.EqualFold(key, "X-Request-ID") {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse(initial http.Header) *bufferedResponse {
	return &bufferedResponse{header: initial.Clone()}
}
func (w *bufferedResponse) Header() http.Header { return w.header }
func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *bufferedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}
