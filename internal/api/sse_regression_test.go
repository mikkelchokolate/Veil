package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type sseProbeWriter struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func newSSEProbeWriter() *sseProbeWriter {
	return &sseProbeWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
}

func (w *sseProbeWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.flushed) })
}

func TestSSEEventTypesUseExactCommaSeparatedTokens(t *testing.T) {
	state := &managementState{}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/v1/events?types=notapply,nottraffic", nil).WithContext(ctx)
	writer := newSSEProbeWriter()
	done := make(chan struct{})
	go func() {
		state.handleV1Events(writer, req)
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not establish stream")
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler ignored cancellation")
	}
	if body := writer.Body.String(); strings.Contains(body, "event: apply") || strings.Contains(body, "event: traffic") {
		t.Fatalf("substring event filter selected an unrequested type: %q", body)
	}
}

func TestSSEConnectionsAreBoundedPerCanonicalAddressAndUser(t *testing.T) {
	const attempts = 256
	const maximumAccepted = 32
	state := &managementState{}
	type stream struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	streams := make([]stream, 0, attempts)
	accepted := 0
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctx = context.WithValue(ctx, contextKeyUsername, "alice")
		req := httptest.NewRequest("GET", "/api/v1/events?types=none", nil).WithContext(ctx)
		req.RemoteAddr = "198.51.100.30:4242"
		writer := newSSEProbeWriter()
		done := make(chan struct{})
		go func() {
			state.handleV1Events(writer, req)
			close(done)
		}()
		select {
		case <-writer.flushed:
			accepted++
			streams = append(streams, stream{cancel: cancel, done: done})
		case <-done:
			cancel()
			if writer.Code != 429 && writer.Code != 503 {
				t.Fatalf("rejected SSE connection status=%d body=%s", writer.Code, writer.Body.String())
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatal("SSE admission decision timed out")
		}
	}
	for _, active := range streams {
		active.cancel()
		select {
		case <-active.done:
		case <-time.After(time.Second):
			t.Fatal("SSE stream did not shut down")
		}
	}
	if accepted > maximumAccepted {
		t.Fatalf("accepted %d concurrent SSE connections for one user/IP; maximum %d", accepted, maximumAccepted)
	}
}
