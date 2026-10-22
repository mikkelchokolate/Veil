package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #1266 "pre-auth registry stat work": any veil_session cookie — an
// arbitrary value suffices — used to make the auth middleware run
// Healthy() (os.Lstat under the registry mutex) and Get() (another
// Lstat, plus a journal-compaction retry when storage had failed) before
// the request is rejected with 401. Unauthenticated traffic that cannot
// name a live session must not reach session storage at all.

func newPreauthWorkRegistry(t *testing.T) (*SessionRegistry, string, Session) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatalf("NewSessionRegistry: %v", err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("session snapshot was not written: %v", err)
	}
	return registry, path, session
}

func TestSessionTokenShapeMatchesIssuedTokens(t *testing.T) {
	_, _, session := newPreauthWorkRegistry(t)
	if !wellFormedSessionToken(session.Token) {
		t.Fatalf("issued token %q is not well-formed; the pre-auth shape check would lock every user out", session.Token)
	}
	for _, bad := range []string{
		"",
		"garbage",
		strings.Repeat("a", 31),
		strings.Repeat("a", 33),
		strings.Repeat("a", 64),
		strings.ToUpper(session.Token),
		strings.Repeat("g", 32),
		session.Token[:31] + " ",
	} {
		if wellFormedSessionToken(bad) {
			t.Errorf("wellFormedSessionToken(%q) = true, want false", bad)
		}
	}
}

func TestAuthMiddlewareSkipsSessionStorageForUnknownCookies(t *testing.T) {
	registry, path, session := newPreauthWorkRegistry(t)
	// Break storage the way storageHealthyLocked detects it: the snapshot
	// path is no longer a regular file. Any request that reaches
	// Healthy() now answers 503, so a 401 proves the middleware never
	// touched session storage for that cookie.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := registry.Healthy(); err == nil {
		t.Fatal("precondition: broken snapshot path must make the registry unhealthy")
	}

	state := &managementState{users: []User{{Username: "alice", Role: "admin"}}, sessions: registry}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authMiddleware(state, "static-secret", next)

	for _, tc := range []struct {
		name   string
		cookie string
		want   int
	}{
		{"malformed value", "garbage", http.StatusUnauthorized},
		{"oversized value", strings.Repeat("z", 4096), http.StatusUnauthorized},
		{"uppercased live token", strings.ToUpper(session.Token), http.StatusUnauthorized},
		{"well-formed unknown token", strings.Repeat("a", 32), http.StatusUnauthorized},
		// A cookie that names a live session still fails closed on broken
		// storage instead of being treated as anonymous.
		{"live token", session.Token, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: tc.cookie})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET /api/status with %s cookie = %d, want %d; body = %s", tc.name, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestSessionRegistryGetUnknownTokenDoesNoStorageWork(t *testing.T) {
	registry, _, session := newPreauthWorkRegistry(t)
	sentinel := errors.New("simulated earlier storage failure")
	registry.mu.Lock()
	registry.storageErr = sentinel
	registry.mu.Unlock()

	// An unknown token cannot be authenticated whatever the storage state,
	// so it must not trigger the journal-compaction recovery (a disk write
	// under the store lock) that storageHealthyLocked runs on storageErr.
	if _, ok := registry.Get(strings.Repeat("b", 32)); ok {
		t.Fatal("unknown token authenticated")
	}
	registry.mu.Lock()
	got := registry.storageErr
	registry.mu.Unlock()
	if got != sentinel {
		t.Fatalf("Get(unknown) ran storage recovery: storageErr = %v, want the untouched sentinel", got)
	}

	// A live token still goes through the health check, which recovers.
	if _, ok := registry.Get(session.Token); !ok {
		t.Fatal("live session not found after recovery")
	}
	registry.mu.Lock()
	got = registry.storageErr
	registry.mu.Unlock()
	if got != nil {
		t.Fatalf("Get(live) did not run storage recovery: storageErr = %v", got)
	}
}

func TestSessionRegistryGetMalformedTokenSkipsRegistryLock(t *testing.T) {
	registry, _, _ := newPreauthWorkRegistry(t)
	registry.mu.Lock()
	done := make(chan bool, 1)
	go func() {
		_, ok := registry.Get("not-a-session-token")
		done <- ok
	}()
	select {
	case ok := <-done:
		registry.mu.Unlock()
		if ok {
			t.Fatal("malformed token authenticated")
		}
	case <-time.After(2 * time.Second):
		registry.mu.Unlock()
		<-done
		t.Fatal("Get(malformed) waited on the registry mutex; malformed cookies must be rejected before it")
	}
}
