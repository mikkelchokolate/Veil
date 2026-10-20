package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

type sseLine struct {
	at   time.Time
	text string
}

// openRevocationProbeStream opens a viewer-session SSE stream through the
// real router + authMiddleware and returns a channel of received lines that
// is closed when the server ends the stream.
func openRevocationProbeStream(t *testing.T, server *httptest.Server, path, sessionToken string) <-chan sseLine {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: sessionToken})
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open %s: status %d", path, resp.StatusCode)
	}
	lines := make(chan sseLine, 256)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- sseLine{at: time.Now(), text: scanner.Text()}
		}
	}()
	return lines
}

func waitForSSEEvent(t *testing.T, lines <-chan sseLine) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream closed before the first event")
			}
			if strings.HasPrefix(line.text, "event: ") {
				return
			}
		case <-deadline:
			t.Fatal("no SSE event within 5s of opening the stream")
		}
	}
}

// TestSSEStreamsCloseWhenTheSessionStopsAuthorizing covers #1290: the
// middleware authenticated /api/v1/events and /api/v1/traffic/stream once, and
// the handler (which clears the server WriteTimeout) then streamed forever —
// logout, password change (revoke-by-username), user delete, second-factor
// enrollment and absolute/idle expiry all left an open stream flowing. Every
// such revocation must now end the stream within one hub tick, while an
// untouched session keeps streaming (control, so a stream that simply dies on
// its own cannot make the revoked cases pass).
func TestSSEStreamsCloseWhenTheSessionStopsAuthorizing(t *testing.T) {
	type mutation func(state *managementState, session Session, clockOffset *atomic.Int64)
	removeViewer := func(state *managementState, _ Session, _ *atomic.Int64) {
		state.mu.Lock()
		defer state.mu.Unlock()
		kept := state.users[:0]
		for _, user := range state.users {
			if user.Username != "viewer" {
				kept = append(kept, user)
			}
		}
		state.users = kept
	}
	armViewerTOTP := func(state *managementState, _ Session, _ *atomic.Int64) {
		state.mu.Lock()
		defer state.mu.Unlock()
		for i := range state.users {
			if state.users[i].Username == "viewer" {
				state.users[i].TOTPEnabled = true
			}
		}
	}
	cases := []struct {
		name    string
		path    string
		mutate  mutation
		revoked bool
	}{
		{name: "control/events", path: "/api/v1/events", mutate: func(*managementState, Session, *atomic.Int64) {}},
		{name: "control/traffic-stream", path: "/api/v1/traffic/stream", mutate: func(*managementState, Session, *atomic.Int64) {}},
		{name: "logout", path: "/api/v1/events", revoked: true, mutate: func(state *managementState, session Session, _ *atomic.Int64) {
			if err := state.sessionRegistry().Delete(session.Token); err != nil {
				t.Error(err)
			}
		}},
		{name: "revoke-by-username", path: "/api/v1/traffic/stream", revoked: true, mutate: func(state *managementState, _ Session, _ *atomic.Int64) {
			if _, err := state.sessionRegistry().DeleteByUsername("viewer"); err != nil {
				t.Error(err)
			}
		}},
		{name: "user-deleted", path: "/api/v1/events", revoked: true, mutate: removeViewer},
		{name: "second-factor-armed", path: "/api/v1/events", revoked: true, mutate: armViewerTOTP},
		{name: "absolute-expiry", path: "/api/v1/events", revoked: true, mutate: func(_ *managementState, _ Session, offset *atomic.Int64) {
			offset.Store(int64(defaultSessionAbsoluteTimeout + time.Minute))
		}},
		{name: "idle-expiry", path: "/api/v1/events", revoked: true, mutate: func(_ *managementState, _ Session, offset *atomic.Int64) {
			offset.Store(int64(defaultSessionIdleTimeout + time.Minute))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := testdb.Open(t)
			t.Cleanup(func() { _ = db.Close() })
			var clockOffset atomic.Int64
			registry := mustNewSessionRegistry("")
			registry.now = func() time.Time { return time.Now().Add(time.Duration(clockOffset.Load())) }
			state := &managementState{
				sessions: registry,
				users: []User{
					{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
					{Username: "viewer", PasswordHash: "viewer-hash", Role: "viewer", Locale: "en"},
				},
				clientRepo:   client.NewRepository(db),
				trafficStore: client.NewTrafficStore(db),
			}
			t.Cleanup(state.CloseStreams)
			session := mustCreateSession(t, registry, "viewer", "viewer")
			mux := http.NewServeMux()
			state.register(mux)
			server := httptest.NewServer(authMiddleware(state, "", mux))
			t.Cleanup(server.Close)

			lines := openRevocationProbeStream(t, server, tc.path, session.Token)
			waitForSSEEvent(t, lines)
			tc.mutate(state, session, &clockOffset)
			mutatedAt := time.Now()

			// The hub refreshes every 2s; a revoked stream must close by the
			// next tick, an authorized one must still deliver events.
			const window = 4500 * time.Millisecond
			eventsAfter := 0
			closed := false
			timeout := time.After(window)
		collect:
			for {
				select {
				case line, ok := <-lines:
					if !ok {
						closed = true
						break collect
					}
					if strings.HasPrefix(line.text, "event: ") && line.at.After(mutatedAt) {
						eventsAfter++
					}
				case <-timeout:
					break collect
				}
			}
			if tc.revoked {
				if !closed {
					t.Fatalf("stream still open %v after %s; %d events delivered post-revocation", window, tc.name, eventsAfter)
				}
				// At most one snapshot (apply + traffic) can already be
				// mid-write when the revocation lands.
				if eventsAfter > 2 {
					t.Fatalf("%d events delivered after %s; want at most one in-flight snapshot", eventsAfter, tc.name)
				}
				return
			}
			if closed {
				t.Fatal("control stream closed although its session is still valid")
			}
			if eventsAfter == 0 {
				t.Fatalf("control stream delivered no events within %v", window)
			}
		})
	}
}
