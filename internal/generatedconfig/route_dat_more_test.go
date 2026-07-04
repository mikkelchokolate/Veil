package generatedconfig

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type timeoutError struct{ msg string }

func (e timeoutError) Error() string   { return e.msg }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return false }

type temporaryReadError struct{}

func (temporaryReadError) Error() string   { return "temporary read error" }
func (temporaryReadError) Temporary() bool { return true }

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

type failingBodyRoundTripper struct {
	err error
}

func (t *failingBodyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(errReader{err: t.err}),
		Request:    req,
	}, nil
}

func TestRouteDatRetryPolicyRetryableTimeout(t *testing.T) {
	policy := NewRouteDatRetryPolicy()
	if !policy.Retryable(timeoutError{msg: "timed out"}) {
		t.Fatal("timeout error should be retryable")
	}
}

func TestRouteDatRetryPolicyRetryableNilAndPlain(t *testing.T) {
	policy := NewRouteDatRetryPolicy()
	if policy.Retryable(nil) {
		t.Fatal("nil error should not be retryable")
	}
	if policy.Retryable(errors.New("plain")) {
		t.Fatal("plain error should not be retryable")
	}
}

func TestRouteDatBodyLimitDefaultsToMaxSize(t *testing.T) {
	limit := NewRouteDatBodyLimit(0)
	if limit.Limit() != maxRouteDatSize {
		t.Fatalf("Limit() = %d, want %d", limit.Limit(), maxRouteDatSize)
	}
}

func TestDownloadRouteDatRetriesOnRetryableReadError(t *testing.T) {
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })
	routeDatHTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &failingBodyRoundTripper{err: temporaryReadError{}},
	}

	_, err := downloadRouteDat("http://example.com/geoip.dat")
	if err == nil {
		t.Fatal("expected error after retries")
	}
}

func TestDownloadRouteDatReturnsNonRetryableReadErrorImmediately(t *testing.T) {
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })
	routeDatHTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &failingBodyRoundTripper{err: errors.New("permanent read error")},
	}

	_, err := downloadRouteDat("http://example.com/geoip.dat")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "permanent read error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadRouteDatRetriesOnServerErrorAndSucceeds(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
