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
	if first.Code != http.StatusAccepted || second.Code != first.Code || second.Body.String() != first.Body.String() || calls.Load() != 1 {
		t.Fatalf("first=%d/%q second=%d/%q calls=%d", first.Code, first.Body.String(), second.Code, second.Body.String(), calls.Load())
	}
	if !strings.Contains(first.Body.String(), "response_too_large") {
		t.Fatalf("expected bounded committed response, got %q", first.Body.String())
	}
	if first.Body.Len() > 1024 {
		t.Fatalf("bounded response unexpectedly large: %d", first.Body.Len())
	}
	if strings.Count(first.Body.String(), "x") > 0 {
		t.Fatalf("truncated payload was returned or cached: %q", first.Body.String())
	}
}

func TestInMemoryIdempotencyCachesResponsesAtBodyLimit(t *testing.T) {
	cases := []struct {
		name     string
		size     int
		writes   int
		wantCode int
	}{
		{name: "limit-minus-one", size: maxIdempotencyBody - 1, writes: 1, wantCode: http.StatusOK},
		{name: "exact-limit", size: maxIdempotencyBody, writes: 1, wantCode: http.StatusOK},
		{name: "limit-plus-one", size: maxIdempotencyBody + 1, writes: 1, wantCode: http.StatusAccepted},
		{name: "multi-write-overflow", size: maxIdempotencyBody + 8, writes: 2, wantCode: http.StatusAccepted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newIdempotencyStore()
			defer store.Close()
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
			if second.Code != first.Code || second.Body.String() != first.Body.String() {
				t.Fatalf("replay mismatch first=%d/%q second=%d/%q", first.Code, first.Body.String(), second.Code, second.Body.String())
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d want 1", calls.Load())
			}
			if tc.wantCode == http.StatusOK && first.Body.Len() != tc.size {
				t.Fatalf("cached body len=%d want=%d", first.Body.Len(), tc.size)
			}
			if tc.wantCode == http.StatusAccepted && first.Body.Len() == tc.size {
				t.Fatalf("overflow response replayed full truncated payload len=%d", first.Body.Len())
			}
		})
	}
}
