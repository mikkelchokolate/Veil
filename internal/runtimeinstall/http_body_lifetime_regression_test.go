package runtimeinstall

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type trackedResponseBody struct {
	io.Reader
	active *atomic.Int32
}

func (b *trackedResponseBody) Close() error {
	b.active.Add(-1)
	return nil
}

type fallbackBodyLifetimeTransport struct {
	active         atomic.Int32
	calls          atomic.Int32
	openAtFallback atomic.Int32
	sentinel       error
}

func (t *fallbackBodyLifetimeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if t.calls.Add(1) == 1 {
		t.active.Add(1)
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     make(http.Header),
			Body:       &trackedResponseBody{Reader: strings.NewReader(`{"message":"rate limited"}`), active: &t.active},
		}, nil
	}
	t.openAtFallback.Store(t.active.Load())
	return nil, t.sentinel
}

func TestReleaseFallbackClosesRateLimitedResponseBeforeNextRequest(t *testing.T) {
	transport := &fallbackBodyLifetimeTransport{sentinel: errors.New("stop after lifetime observation")}
	client := &http.Client{Transport: transport}
	_, err := fetchReleaseAt(t.Context(), client, "https://api.github.test/releases/latest", "owner/repo")
	if !errors.Is(err, transport.sentinel) {
		t.Fatalf("unexpected fallback result: %v", err)
	}
	if got := transport.openAtFallback.Load(); got != 0 {
		t.Fatalf("rate-limited response bodies still open when fallback request began: %d", got)
	}
	if got := transport.active.Load(); got != 0 {
		t.Fatalf("response body leaked after fallback returned: %d", got)
	}
}
