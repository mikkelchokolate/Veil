package api

import (
	"bytes"
	"encoding/json"
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
	first := issue()
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	// The transient 5xx must not be cached: the same key retries the handler.
	second := issue()
	if second.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry status=%d body=%s calls=%d", second.Code, second.Body.String(), calls.Load())
	}
	if second.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("retried response must not be marked replayed: %v", second.Header())
	}
	// The committed outcome IS cached: a third identical request replays the
	// recorded success without running the mutation again.
	third := issue()
	if third.Code != http.StatusCreated || third.Body.String() != second.Body.String() {
		t.Fatalf("third replay status=%d body=%s, want identical to %d/%s", third.Code, third.Body.String(), second.Code, second.Body.String())
	}
	if third.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("third response must carry Idempotency-Replayed: true, got %v", third.Header())
	}
	if calls.Load() != 2 {
		t.Fatalf("cached success was not replayed: calls=%d", calls.Load())
	}
}

func TestIdempotencyOversizeResponseIsBoundedAndReplayIdentical(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
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
	if first.Code != http.StatusAccepted {
		t.Fatalf("oversized success must be bounded to 202, got %d body=%q", first.Code, first.Body.String())
	}
	assertResponseTooLargeEnvelope(t, first)
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
		t.Fatalf("oversized response repeated the mutation: calls=%d", calls.Load())
	}
	// The durable record must hold the bounded envelope — never the truncated
	// payload — so a later replay cannot serve bytes the handler never meant.
	var storedStatus int
	var storedBody []byte
	if err := db.QueryRow(`SELECT response_status,response_body FROM idempotency_results`).Scan(&storedStatus, &storedBody); err != nil {
		t.Fatalf("read durable idempotency result: %v", err)
	}
	if storedStatus != http.StatusAccepted || strings.Contains(string(storedBody), "x") ||
		!strings.Contains(string(storedBody), "response_too_large") {
		t.Fatalf("durable record stored wrong body: status=%d body=%q", storedStatus, storedBody)
	}
}

// assertResponseTooLargeEnvelope locks the exact bounded envelope: 202 with a
// small JSON body identifying the committed-but-too-large outcome and no
// fragment of the truncated payload.
func assertResponseTooLargeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Body.Len() > 1024 {
		t.Fatalf("bounded response unexpectedly large: %d", rec.Body.Len())
	}
	var envelope struct {
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("bounded response is not the JSON envelope: %q", rec.Body.String())
	}
	if envelope.Status != "committed" || envelope.Result != "response_too_large" {
		t.Fatalf("unexpected bounded envelope: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "x") {
		t.Fatalf("bounded response leaked truncated payload: %q", rec.Body.String())
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
