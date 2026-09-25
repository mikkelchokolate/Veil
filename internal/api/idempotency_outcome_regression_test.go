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

// #1058/#1003: a transient 423 restore-lock refusal is "not attempted, try
// later" — it must never be durably stored as the operation's terminal
// result, or every same-key retry replays Locked forever while telling the
// client to retry.
func TestIdempotencyDoesNotCacheTransient423Refusal(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "5")
			writeError(w, "management mutation is locked while restore is in progress", http.StatusLocked)
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"committed": true})
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/test/restore-lock", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "restore-locked")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := issue()
	if first.Code != http.StatusLocked {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	// The transient refusal must have released the reservation: nothing may be
	// left in the durable store for a later retry to replay.
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM idempotency_records`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("transient 423 refusal was persisted; idempotency_records=%d", stored)
	}
	// After the lock lifts the identical request executes the mutation.
	second := issue()
	if second.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry status=%d body=%s calls=%d", second.Code, second.Body.String(), calls.Load())
	}
	if second.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("retried response must not be marked replayed: %v", second.Header())
	}
	// The real outcome is terminal and IS cached — a third identical request
	// replays it without re-running the mutation.
	third := issue()
	if third.Code != http.StatusCreated || third.Body.String() != second.Body.String() {
		t.Fatalf("third replay status=%d body=%s, want identical to %d/%s", third.Code, third.Body.String(), second.Code, second.Body.String())
	}
	if third.Header().Get("Idempotency-Replayed") != "true" || calls.Load() != 2 {
		t.Fatalf("committed result was not durably replayed: replayed=%v calls=%d", third.Header(), calls.Load())
	}
}

// #1057: when the mutation committed inside the domain transaction but the
// handler then fails with 5xx (post-commit read), aborting must NOT delete
// the idempotency record — that would erase the only proof the operation ran
// and a same-key retry would re-execute a non-idempotent mutation. The record
// is settled as committed_response_pending and the retry replays it.
func TestIdempotencyAbortAfterCommittedMutationDoesNotReExecute(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		operation, ok := idempotencyDomainOperationFromContext(r.Context())
		if !ok {
			http.Error(w, "missing domain operation", http.StatusInternalServerError)
			return
		}
		// Simulate withClientMutation's domain binding: the client mutation +
		// revision committed in one tx; only the post-commit response read
		// fails.
		if _, err := db.Exec(`UPDATE domain_operations SET state='mutation_committed',domain_result_json=? WHERE id=? AND scope=? AND operation_generation=?`,
			`{"revision":7}`, operation.ID, operation.Scope, operation.Generation); err != nil {
			http.Error(w, "bind failed", http.StatusInternalServerError)
			return
		}
		http.Error(w, "post-commit read failed", http.StatusInternalServerError)
	}))
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/test/commit-then-500", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "commit-then-500")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := issue()
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	// The record must be settled completed (committed_response_pending), not
	// deleted — deleting would let the retry below re-run the mutation.
	var state, outcome string
	if err := db.QueryRow(`SELECT state,outcome_class FROM idempotency_records`).Scan(&state, &outcome); err != nil {
		t.Fatalf("committed mutation's idempotency record was deleted: %v", err)
	}
	if state != "completed" || outcome != "committed_response_pending" {
		t.Fatalf("record state=%q outcome=%q, want completed/committed_response_pending", state, outcome)
	}
	second := issue()
	if second.Code != http.StatusAccepted || !strings.Contains(second.Body.String(), `"revision":7`) {
		t.Fatalf("retry status=%d body=%s, want replayed committed_response_pending", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("retry must carry Idempotency-Replayed: true, got %v", second.Header())
	}
	if calls.Load() != 1 {
		t.Fatalf("committed mutation re-executed on retry: calls=%d", calls.Load())
	}
}

// #1039: a handler panic must release the durable reservation. Without the
// deferred settle, the leaked heartbeat keeps the lease fresh forever and the
// key wedges on 409 "idempotent operation is still pending" until restart.
func TestIdempotencyHandlerPanicReleasesDurableReservation(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			panic("handler exploded")
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"committed": true})
	}))
	issue := func() (response *httptest.ResponseRecorder, panicked bool) {
		response = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/test/panic", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "panic")
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		handler.ServeHTTP(response, req)
		return response, false
	}
	if _, panicked := issue(); !panicked {
		t.Fatal("handler panic did not propagate through the middleware")
	}
	// The reservation must be released, not left 'reserved' with a live
	// heartbeat lease.
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM idempotency_records`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("panicked request left a wedged reservation; idempotency_records=%d", stored)
	}
	// The same key retries cleanly and re-executes the mutation.
	second, panicked := issue()
	if panicked || second.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry status=%v panicked=%v calls=%d", second.Code, panicked, calls.Load())
	}
	third, panicked := issue()
	if panicked || third.Code != http.StatusCreated || third.Body.String() != second.Body.String() {
		t.Fatalf("third replay status=%v body=%s panicked=%v", third.Code, third.Body.String(), panicked)
	}
	if third.Header().Get("Idempotency-Replayed") != "true" || calls.Load() != 2 {
		t.Fatalf("completed result was not durably replayed: replayed=%v calls=%d", third.Header(), calls.Load())
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
