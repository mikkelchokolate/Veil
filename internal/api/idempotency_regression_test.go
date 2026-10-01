package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdempotencyKeyReplaysWithoutRepeatingMutation(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("X-Result", "created")
		writeJSONStatus(w, http.StatusCreated, map[string]any{"call": n})
	}))
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "create-client-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	first := request(`{"name":"a"}`)
	second := request(`{"name":"a"}`)
	if calls.Load() != 1 || first.Code != http.StatusCreated || second.Code != first.Code || second.Body.String() != first.Body.String() || second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("calls=%d first=%d/%s second=%d/%s replay=%q", calls.Load(), first.Code, first.Body.String(), second.Code, second.Body.String(), second.Header().Get("Idempotency-Replayed"))
	}
	conflict := request(`{"name":"different"}`)
	if conflict.Code != http.StatusConflict || calls.Load() != 1 {
		t.Fatalf("conflict status=%d calls=%d body=%s", conflict.Code, calls.Load(), conflict.Body.String())
	}
	if conflict.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("conflict must not be marked replayed: %v", conflict.Header())
	}
	if !strings.Contains(conflict.Body.String(), `"error"`) ||
		!strings.Contains(conflict.Body.String(), "Idempotency-Key") {
		t.Fatalf("conflict response missing error envelope: %s", conflict.Body.String())
	}
}

func TestIdempotencyKeyCoalescesConcurrentMutation(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		writeJSONStatus(w, http.StatusCreated, map[string]int{"call": 1})
	}))
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodDelete, "/api/inbounds/edge", nil)
			req.Header.Set("Idempotency-Key", "delete-edge")
			responses[i] = httptest.NewRecorder()
			handler.ServeHTTP(responses[i], req)
		}(i)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 || responses[0].Code != http.StatusCreated || responses[1].Code != http.StatusCreated || responses[0].Body.String() != responses[1].Body.String() {
		t.Fatalf("calls=%d responses=%v/%v", calls.Load(), responses[0], responses[1])
	}
	// Coalescing is observable: the loser waited for the winner's outcome and
	// replays it — exactly one response carries the replay marker.
	replays := 0
	for _, response := range responses {
		if response.Header().Get("Idempotency-Replayed") == "true" {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("coalesced replay markers=%d want=1 responses=%v/%v", replays, responses[0].Header(), responses[1].Header())
	}
}

func TestMutationWithoutIdempotencyKeyIsNotCached(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	var calls int
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, calls)
	}))
	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/routing/presets/direct", nil))
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

// A stored replay must never re-serve Set-Cookie — the durable store
// already drops it and the in-memory path now matches (#1221, #1222).
// The FIRST response still delivers its cookie; only the replay strips it.
func TestInMemoryIdempotencyReplayStripsSetCookie(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "veil_session", Value: "stale-token"})
		w.Header().Set("X-Result", "ok")
		writeJSONStatus(w, http.StatusOK, map[string]any{"ok": true})
	}))
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp/verify", nil)
		req.Header.Set("Idempotency-Key", "verify-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if first := request(); cookieValue(first, "veil_session") != "stale-token" {
		t.Fatal("first response lost its Set-Cookie")
	}
	replay := request()
	if replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("expected replay, got %v", replay.Header())
	}
	if got := replay.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("replay re-served Set-Cookie: %q", got)
	}
	if replay.Header().Get("X-Result") != "ok" {
		t.Fatal("replay dropped a normal response header")
	}
}

// Secret-marked responses get the short replay TTL — the in-memory
// analogue of the durable store's encrypted 5-minute envelope — and the
// internal X-Veil-Internal-* marking headers never reach the wire or the
// stored record (#1221). Unmarked responses keep the 24h public TTL.
func TestInMemoryIdempotencySecretResponseExpiresEarly(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	now := time.Now()
	store.now = func() time.Time { return now }
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		markIdempotencySecretResponse(w, "totp-enroll:alice", 1)
		writeJSONStatus(w, http.StatusOK, map[string]any{"secret": "SEED"})
	}))
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/totp/enroll", nil)
		req.Header.Set("Idempotency-Key", "enroll-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	for _, header := range []string{idempotencySensitivityHeader, idempotencyResourceHeader, idempotencySecretGenerationHeader, idempotencyOutcomeHeader} {
		if got := first.Header().Get(header); got != "" {
			t.Fatalf("internal header %s leaked to the client: %q", header, got)
		}
	}
	if replay := request(); replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("secret response did not replay inside its TTL")
	}
	now = now.Add(secretReplayTTL + time.Second)
	if third := request(); third.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("secret entry replayed past its 5-minute TTL")
	}

	// Control: an unmarked response still replays well past secretReplayTTL.
	var calls atomic.Int32
	public := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(w, http.StatusOK, map[string]any{"call": calls.Add(1)})
	}))
	pubReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", nil)
		req.Header.Set("Idempotency-Key", "public-1")
		rec := httptest.NewRecorder()
		public.ServeHTTP(rec, req)
		return rec
	}
	pubReq()
	now = now.Add(secretReplayTTL + time.Minute)
	if rec := pubReq(); rec.Header().Get("Idempotency-Replayed") != "true" || calls.Load() != 1 {
		t.Fatalf("public entry expired early: replay=%q calls=%d", rec.Header().Get("Idempotency-Replayed"), calls.Load())
	}
}

// Factor endpoints must mark their secret-bearing responses so BOTH
// idempotency backends route them into the encrypted short-TTL lane
// (#1221). The TOTP enroll body carries the seed; a second-factor login
// verify mints a session.
func TestFactorEndpointsMarkSecretIdempotentResponses(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, now := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	store := newIdempotencyStore()
	defer store.Close()

	// TOTP enroll (seed + otpauth URI) through the middleware.
	enroll := store.Middleware(http.HandlerFunc(state.handleMyTOTPEnroll))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/totp/enroll", nil)
	req.Header.Set("Idempotency-Key", "enroll-secret")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, "alice"))
	rec := httptest.NewRecorder()
	enroll.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", rec.Code, rec.Body.String())
	}

	// TOTP login verify (session-minting body) through the middleware.
	verify := store.Middleware(http.HandlerFunc(state.handleTOTPVerify))
	pending := cookieValue(totpLogin(t, state, "correct-password-123"), pendingSecondFactorCookie)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp/verify", strings.NewReader(
		`{"code":"`+totpCode(t, "JBSWY3DPEHPK3PXP", *now)+`"}`))
	req.RemoteAddr = "203.0.113.9:443"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "verify-secret")
	req.AddCookie(&http.Cookie{Name: pendingSecondFactorCookie, Value: pending})
	rec = httptest.NewRecorder()
	verify.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", rec.Code, rec.Body.String())
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(store.entries))
	}
	for scope, entry := range store.entries {
		if !entry.secret {
			t.Fatalf("factor response %q stored without the secret mark", scope)
		}
		if entry.header.Get(idempotencySensitivityHeader) != "" || entry.header.Get("Set-Cookie") != "" {
			t.Fatalf("stored record for %q leaked internal/cookie headers: %v", scope, entry.header)
		}
	}
}
