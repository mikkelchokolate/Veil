package generatedconfig

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veil-panel/veil/internal/secrets"
)

var _managementTestDeps_route_dat = []any{
	bytes.Buffer{}, rand.Reader, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, time.Second, secrets.IsEncrypted,
}

func TestDownloadRouteDatReturnsBodyOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("route-dat-content"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "route-dat-content" {
		t.Fatalf("expected route-dat-content, got %q", string(body))
	}
}

func TestDownloadRouteDatReturnsErrorOnNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestDownloadRouteDatReturnsErrorOnInvalidURL(t *testing.T) {
	_, err := downloadRouteDat("://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// retryTestTransport is a http.RoundTripper that fails the first failCount
// requests with a connection error, then delegates to the base transport.
type retryTestTransport struct {
	attempts  *int
	failCount int
	base      http.RoundTripper
}

func (t *retryTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*t.attempts++
	if *t.attempts <= t.failCount {
		return nil, &testNetworkError{msg: "simulated connection refused"}
	}
	return t.base.RoundTrip(req)
}

type testNetworkError struct{ msg string }

func (e *testNetworkError) Error() string   { return e.msg }
func (e *testNetworkError) Timeout() bool   { return false }
func (e *testNetworkError) Temporary() bool { return true }

type timeoutRecordingTransport struct {
	onRoundTrip func(*http.Request)
}

func (t *timeoutRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.onRoundTrip != nil {
		t.onRoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestDownloadRouteDatRetriesOnServerError(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success-after-retries"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if string(body) != "success-after-retries" {
		t.Fatalf("expected success-after-retries, got %q", string(body))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDownloadRouteDatRetriesOnConnectionRefused(t *testing.T) {
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success-after-conn-retries"))
	}))
	defer server.Close()

	routeDatHTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &retryTestTransport{
			attempts:  &attempts,
			failCount: 2,
			base:      server.Client().Transport,
		},
	}

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "success-after-conn-retries" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDownloadRouteDatGivesUpAfterMaxRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts before giving up, got %d", attempts)
	}
}

func TestDownloadRouteDatNoRetryOn4xx(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestDownloadRouteDatLogsRetries(t *testing.T) {
	var buf bytes.Buffer
	oldLogger := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldLogger) })

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

	_, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "retry") && !strings.Contains(logOutput, "Retry") && !strings.Contains(logOutput, "attempt") {
		t.Fatalf("expected retry message in log output, got: %s", logOutput)
	}
}

func TestVerifyRouteDatChecksumSuccessWithStandardFormat(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessWithSha256Prefix(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "sha256:3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error with sha256 prefix: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessMultipleEntries(t *testing.T) {
	geositeBody := []byte("different content for geosite")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6  geoip.dat\n" +
		"8bfd86422903167e5f93020206abdfdd52a2ae3cdb76e3e4fbafa586a043a50a  geosite.dat\n"

	err := verifyRouteDatChecksum("geosite.dat", geositeBody, checksumText)
	if err != nil {
		t.Fatalf("unexpected error for second entry: %v", err)
	}
}

func TestVerifyRouteDatChecksumSuccessFallbackToFirstToken(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "3a4eee3f4b7c80b43a36c56ad857be4213eaad22d2e02b8efff7b1d095f2a6d6\n"

	err := verifyRouteDatChecksum("other-file.dat", body, checksumText)
	if err != nil {
		t.Fatalf("unexpected error when falling back to first token: %v", err)
	}
}

func TestVerifyRouteDatChecksumMismatch(t *testing.T) {
	body := []byte("route dat content for geoip")
	checksumText := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected 'checksum mismatch' error, got: %v", err)
	}
}

func TestVerifyRouteDatChecksumEmptyText(t *testing.T) {
	body := []byte("content")
	err := verifyRouteDatChecksum("geoip.dat", body, "")
	if err == nil {
		t.Fatal("expected error for empty checksum text, got nil")
	}
	if !strings.Contains(err.Error(), "checksum for geoip.dat is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRouteDatChecksumWhitespaceOnly(t *testing.T) {
	body := []byte("content")
	err := verifyRouteDatChecksum("geoip.dat", body, "  \n\t  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only checksum, got nil")
	}
}

func TestVerifyRouteDatChecksumInvalidHex(t *testing.T) {
	body := []byte("content")
	checksumText := "not-a-valid-hex-string!!!  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected error for invalid hex checksum, got nil")
	}
	if !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("expected 'invalid checksum' error, got: %v", err)
	}
}

func TestVerifyRouteDatChecksumWrongLengthHex(t *testing.T) {
	body := []byte("content")
	// Only 8 hex chars instead of 64 (32 bytes)
	checksumText := "deadbeef  geoip.dat\n"

	err := verifyRouteDatChecksum("geoip.dat", body, checksumText)
	if err == nil {
		t.Fatal("expected error for wrong-length hex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("expected 'invalid checksum' error, got: %v", err)
	}
}

func TestDownloadRouteDatUsesHttpClientWithTimeout(t *testing.T) {
	// Verify the default client has a finite timeout
	if routeDatHTTPClient.Timeout == 0 {
		t.Fatal("routeDatHTTPClient should have a non-zero timeout")
	}

	// Verify timeout is applied to requests by using a custom transport
	// that records whether the request context has a deadline
	var hasDeadline bool
	oldClient := routeDatHTTPClient
	t.Cleanup(func() { routeDatHTTPClient = oldClient })

	routeDatHTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &timeoutRecordingTransport{
			onRoundTrip: func(req *http.Request) {
				_, ok := req.Context().Deadline()
				hasDeadline = ok
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasDeadline {
		t.Fatal("expected request context to have a deadline (timeout) but it did not")
	}
}

func TestDownloadRouteDatRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write content exceeding the 50 MB limit
		chunk := make([]byte, 1024*1024) // 1 MB chunks
		for i := 0; i < 51; i++ {        // 51 MB total > 50 MB limit
			_, _ = w.Write(chunk)
		}
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for oversized body (>50MB), got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") && !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected error to mention size limit, got: %v", err)
	}
}

func TestDownloadRouteDatWithinSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("small-body"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "small-body" {
		t.Fatalf("expected small-body, got %q", string(body))
	}
}
