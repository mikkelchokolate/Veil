package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIdempotencyDoesNotCacheTransientPrecommit5xx(t *testing.T) {
	db := openApplyTestDB(t)
	store := newIdempotencyStore(db)
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"committed": true})
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/test/transient", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "transient")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if first := issue(); first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	if second := issue(); second.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry status=%d body=%s calls=%d", second.Code, second.Body.String(), calls.Load())
	}
}

func TestIdempotencyOversizeResponseIsBoundedAndReplayIdentical(t *testing.T) {
	db := openApplyTestDB(t)
	store := newIdempotencyStore(db)
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxIdempotencyBody+4096))
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/test/large", strings.NewReader(`{"a":1}`))
		req.Header.Set("Idempotency-Key", "large")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := issue()
	second := issue()
	if first.Code != http.StatusAccepted || second.Code != first.Code || second.Body.String() != first.Body.String() || calls.Load() != 1 {
		t.Fatalf("first=%d/%q second=%d/%q calls=%d", first.Code, first.Body.String(), second.Code, second.Body.String(), calls.Load())
	}
	if first.Body.Len() > 1024 {
		t.Fatalf("bounded response unexpectedly large: %d", first.Body.Len())
	}
}

func TestIdempotencyFingerprintCanonicalizesJSONAndQueryOrdering(t *testing.T) {
	requestA := httptest.NewRequest(http.MethodPost, "/api/test?b=2&a=3&a=1", strings.NewReader(`{"b":2,"a":1}`))
	requestA.Header.Set("Content-Type", "application/json")
	requestB := httptest.NewRequest(http.MethodPost, "/api/test?a=1&a=3&b=2", strings.NewReader("{\n  \"a\": 1, \"b\": 2\n}"))
	requestB.Header.Set("Content-Type", "application/json")
	bodyA, _ := io.ReadAll(requestA.Body)
	bodyB, _ := io.ReadAll(requestB.Body)
	if gotA, gotB := idempotencyFingerprint(requestA, bodyA), idempotencyFingerprint(requestB, bodyB); gotA != gotB {
		t.Fatalf("canonical fingerprints differ: %s != %s", gotA, gotB)
	}
}
