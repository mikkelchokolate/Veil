package api

import (
	"bytes"
	"log"
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

// lockedLogBuffer guards the captured log stream while the heartbeat
// goroutine writes to it; log.SetOutput is process-global and the goroutine
// outlives the read loop, so a plain bytes.Buffer races under -race.
type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureHeartbeatLogs(t *testing.T) *lockedLogBuffer {
	t.Helper()
	logBuf := &lockedLogBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logBuf)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	return logBuf
}

func TestDurableHeartbeatSurfacesExecFailure(t *testing.T) {
	db := openApplyTestDB(t)
	store := newIdempotencyStore(db)
	defer store.Close()
	store.reservationTTL = 150 * time.Millisecond

	logBuf := captureHeartbeatLogs(t)

	stop := store.startDurableHeartbeat("scope", "fingerprint", durableIdempotencyRecord{Generation: 1})
	defer stop()
	// Close the database underneath the heartbeat: a discarded Exec error would
	// leave the lease silently expiring while the caller assumes it is live.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logBuf.String(), "idempotency heartbeat") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("heartbeat Exec failure was discarded instead of logged")
}

func TestDurableHeartbeatStopsAfterOwnershipLoss(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()
	store.reservationTTL = 150 * time.Millisecond

	logBuf := captureHeartbeatLogs(t)

	// No matching reserved row exists, so the UPDATE affects zero rows: the
	// reservation was taken over or removed and the heartbeat must stop
	// instead of reporting a live lease.
	stop := store.startDurableHeartbeat("scope", "fingerprint", durableIdempotencyRecord{Generation: 1})
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logBuf.String(), "ownership lost") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("heartbeat kept running after reservation ownership was lost")
}
