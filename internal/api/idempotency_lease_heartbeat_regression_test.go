package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpiredIdempotencyReservationCannotRunReplacementConcurrently(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()

	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		current := active.Add(1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		writeJSONStatus(w, http.StatusCreated, map[string]any{"ok": true})
	})
	wrapped := store.Middleware(handler)

	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/clients", strings.NewReader(`{"name":"ttl"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "ttl-owner")
		return r
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrapped.ServeHTTP(httptest.NewRecorder(), request())
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		wg.Wait()
		t.Fatal("first owner did not enter handler")
	}
	if _, err := db.Exec(`UPDATE idempotency_records SET reserved_until=0 WHERE state='reserved'`); err != nil {
		close(release)
		wg.Wait()
		t.Fatal(err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrapped.ServeHTTP(httptest.NewRecorder(), request())
	}()
	select {
	case <-entered:
		// A replacement reached the domain mutation while the old owner was live.
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("concurrent domain mutations=%d calls=%d, want exactly one owner", got, calls.Load())
	}
}
