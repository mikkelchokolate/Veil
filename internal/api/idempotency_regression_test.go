package api

import (
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
