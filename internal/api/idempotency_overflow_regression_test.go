package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInMemoryIdempotencyDoesNotTruncateOrReplayOversizedSuccess(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxIdempotencyBody+1))
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/test/large", strings.NewReader(`{"a":1}`))
		req.Header.Set("Idempotency-Key", "large-memory")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := issue()
	second := issue()
	if first.Code != http.StatusAccepted {
		t.Fatalf("oversized success must be bounded to 202, got %d body=%q", first.Code, first.Body.String())
	}
	assertResponseTooLargeEnvelope(t, first)
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("replay mismatch first=%d/%q second=%d/%q", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay must carry Idempotency-Replayed: true, got %v", second.Header())
	}
	if calls.Load() != 1 {
		t.Fatalf("oversized response repeated the mutation: calls=%d", calls.Load())
	}
}

// idempotencyBodyLimitCase exercises one response-size boundary.
type idempotencyBodyLimitCase struct {
	name     string
	size     int
	writes   int
	wantCode int
}

func idempotencyBodyLimitCases() []idempotencyBodyLimitCase {
	return []idempotencyBodyLimitCase{
		{name: "limit-minus-one", size: maxIdempotencyBody - 1, writes: 1, wantCode: http.StatusOK},
		{name: "exact-limit", size: maxIdempotencyBody, writes: 1, wantCode: http.StatusOK},
		{name: "limit-plus-one", size: maxIdempotencyBody + 1, writes: 1, wantCode: http.StatusAccepted},
		{name: "multi-write-overflow", size: maxIdempotencyBody + 8, writes: 2, wantCode: http.StatusAccepted},
	}
}

// runIdempotencyBodyLimitCase drives one boundary case against the given
// store and asserts the exact contract: responses at or under the limit are
// replayed byte-identical; anything over — including multi-write overflow —
// is replaced by the bounded response_too_large envelope, never a truncated
// payload. The mutation runs exactly once.
func runIdempotencyBodyLimitCase(t *testing.T, store *idempotencyStore, tc idempotencyBodyLimitCase) {
	t.Helper()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		payload := bytes.Repeat([]byte("y"), tc.size)
		if tc.writes == 1 {
			_, _ = w.Write(payload)
			return
		}
		split := tc.size / 2
		_, _ = w.Write(payload[:split])
		_, _ = w.Write(payload[split:])
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/test/limit", strings.NewReader(`{"a":1}`))
		req.Header.Set("Idempotency-Key", "limit-"+tc.name)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := issue()
	second := issue()
	if first.Code != tc.wantCode {
		t.Fatalf("first status=%d want=%d body=%q", first.Code, tc.wantCode, first.Body.String())
	}
	if first.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("first response must not be marked replayed: %v", first.Header())
	}
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("replay mismatch first=%d/%q second=%d/%q", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay must carry Idempotency-Replayed: true, got %v", second.Header())
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want 1", calls.Load())
	}
	if tc.wantCode == http.StatusOK {
		// At or under the limit the cached body is the handler's full payload,
		// replayed byte-identical.
		want := strings.Repeat("y", tc.size)
		if first.Body.String() != want || second.Body.String() != want {
			t.Fatalf("cached body len=%d want=%d full payload replayed", first.Body.Len(), tc.size)
		}
		return
	}
	// Overflow responses are replaced by the bounded envelope — the stored
	// record must never contain a fragment of the real payload.
	assertResponseTooLargeEnvelope(t, first)
	assertResponseTooLargeEnvelope(t, second)
}

func TestInMemoryIdempotencyCachesResponsesAtBodyLimit(t *testing.T) {
	for _, tc := range idempotencyBodyLimitCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := newIdempotencyStore()
			defer store.Close()
			runIdempotencyBodyLimitCase(t, store, tc)
		})
	}
}

func TestDurableIdempotencyCachesResponsesAtBodyLimit(t *testing.T) {
	for _, tc := range idempotencyBodyLimitCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := openApplyTestDB(t)
			defer db.Close()
			store := newIdempotencyStore(db)
			defer store.Close()
			runIdempotencyBodyLimitCase(t, store, tc)
		})
	}
}
