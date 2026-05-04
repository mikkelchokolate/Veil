package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHysteria2ReleaseAssetURL(t *testing.T) {
	url, err := Hysteria2ReleaseAssetURL("v2.6.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://github.com/apernet/hysteria/releases/download/app%2Fv2.6.0/hysteria-linux-amd64"
	if url != want {
		t.Fatalf("unexpected url:\n got: %s\nwant: %s", url, want)
	}
}

func TestHysteria2ReleaseAssetURLRejectsUnsupportedArch(t *testing.T) {
	_, err := Hysteria2ReleaseAssetURL("v2.6.0", "linux", "mips")
	if err == nil {
		t.Fatalf("expected unsupported arch error")
	}
}

func TestVerifySHA256HexAcceptsMatchingHash(t *testing.T) {
	got, err := SHA256Hex([]byte("veil"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "01979b66ee1794c473f53cafff0889e714aac28b2515e3572072424b919634f3" {
		t.Fatalf("unexpected sha256: %s", got)
	}
	if err := VerifySHA256Hex([]byte("veil"), got); err != nil {
		t.Fatalf("expected matching hash: %v", err)
	}
}

func TestVerifySHA256HexRejectsMismatch(t *testing.T) {
	if err := VerifySHA256Hex([]byte("veil"), "deadbeef"); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestCaddyNaiveBuildHint(t *testing.T) {
	hint := CaddyNaiveBuildHint("/usr/local/bin/caddy")
	if hint.BinaryPath != "/usr/local/bin/caddy" {
		t.Fatalf("unexpected binary path: %+v", hint)
	}
	if len(hint.Commands) == 0 {
		t.Fatalf("expected commands")
	}
}

func TestDownloadVerifiedBinaryWritesExecutableFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hysteria" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("binary-body"))
	}))
	defer server.Close()
	expected, err := SHA256Hex([]byte("binary-body"))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "hysteria")

	result, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{URL: server.URL + "/hysteria", Destination: dest, SHA256: expected, Mode: 0o755})

	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if result.Destination != dest || result.SHA256 != expected || result.Bytes != int64(len("binary-body")) {
		t.Fatalf("unexpected result: %+v", result)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(body) != "binary-body" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755, got %v", info.Mode().Perm())
	}
}

func TestDownloadVerifiedBinaryRejectsChecksumMismatchAndPreservesExistingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new-body"))
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "hysteria")
	if err := os.WriteFile(dest, []byte("old-body"), 0o755); err != nil {
		t.Fatalf("write existing dest: %v", err)
	}

	_, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{URL: server.URL, Destination: dest, SHA256: "deadbeef", Mode: 0o755})

	if err == nil {
		t.Fatalf("expected checksum mismatch")
	}
	body, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read dest: %v", readErr)
	}
	if string(body) != "old-body" {
		t.Fatalf("checksum mismatch should preserve existing dest, got %q", string(body))
	}
}

func TestDownloadVerifiedBinaryRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write content exceeding the 100 MB limit
		chunk := make([]byte, 1024*1024) // 1 MB chunks
		for i := 0; i < 101; i++ {       // 101 MB total > 100 MB limit
			_, _ = w.Write(chunk)
		}
	}))
	defer server.Close()

	// Use a known hash that won't be reached because body is rejected first
	_, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{
		URL:         server.URL,
		Destination: filepath.Join(t.TempDir(), "bin"),
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected error for oversized body (>100MB), got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") && !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected error to mention size limit, got: %v", err)
	}
}

func TestDownloadVerifiedBinaryRequiresChecksum(t *testing.T) {
	_, err := DownloadVerifiedBinary(context.Background(), http.DefaultClient, DownloadRequest{URL: "https://example.com/bin", Destination: filepath.Join(t.TempDir(), "bin")})
	if err == nil {
		t.Fatalf("expected checksum requirement error")
	}
}

func TestDownloadVerifiedBinaryRetriesOnServerError(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("binary-body"))
	}))
	defer server.Close()

	expected, err := SHA256Hex([]byte("binary-body"))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "binary")

	result, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{
		URL:         server.URL,
		Destination: dest,
		SHA256:      expected,
		Mode:        0o755,
	})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if result.Bytes != int64(len("binary-body")) {
		t.Fatalf("expected %d bytes, got %d", len("binary-body"), result.Bytes)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDownloadVerifiedBinaryGivesUpAfterMaxRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "binary")
	_, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{
		URL:         server.URL,
		Destination: dest,
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts before giving up, got %d", attempts)
	}
}

func TestDownloadVerifiedBinaryNoRetryOn4xx(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "binary")
	_, err := DownloadVerifiedBinary(context.Background(), server.Client(), DownloadRequest{
		URL:         server.URL,
		Destination: dest,
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestDownloadVerifiedBinaryRespectsContextCancellation(t *testing.T) {
	var attempts int
	// Block server to ensure first attempt completes (with 500) before we cancel
	serverReady := make(chan struct{})
	firstAttemptDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-serverReady
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		if attempts == 1 {
			close(firstAttemptDone)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	dest := filepath.Join(t.TempDir(), "binary")
	errCh := make(chan error, 1)
	go func() {
		_, err := DownloadVerifiedBinary(ctx, server.Client(), DownloadRequest{
			URL:         server.URL,
			Destination: dest,
			SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		})
		errCh <- err
	}()

	// Let first attempt complete
	close(serverReady)
	<-firstAttemptDone
	// Cancel context during backoff sleep (before second attempt)
	cancel()

	err := <-errCh
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
	if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "Canceled") {
		t.Fatalf("expected context cancellation error, got: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (context cancelled during backoff), got %d", attempts)
	}
}
