package privileged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
)

func TestRunSyncCaddyCertCopiesPairToOutDir(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	root := t.TempDir()
	certPath := filepath.Join(root, "certs", "acme-v2", "example.com", "example.com.crt")
	keyPath := filepath.Join(root, "certs", "acme-v2", "example.com", "example.com.key")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("cert-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	findCaddyCertPair = func(_, domain string) (caddycert.Pair, error) {
		if domain == "example.com" {
			return caddycert.Pair{CertPath: certPath, KeyPath: keyPath}, nil
		}
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}

	result, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: root}, ProductionConfig{})
	if err != nil {
		t.Fatalf("sync caddy cert: %v", err)
	}
	if !result.Found {
		t.Fatal("expected cert to be found")
	}
	if got, err := os.ReadFile(result.CertPath); err != nil || string(got) != "cert-data" {
		t.Fatalf("cert path=%q data=%q err=%v", result.CertPath, got, err)
	}
	if got, err := os.ReadFile(result.KeyPath); err != nil || string(got) != "key-data" {
		t.Fatalf("key path=%q data=%q err=%v", result.KeyPath, got, err)
	}
}

func TestRunSyncCaddyCertRequiresDomain(t *testing.T) {
	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected domain required error")
	}
}

func TestRunSyncCaddyCertDefaultsOutDir(t *testing.T) {
	originalFinder := findCaddyCertPair
	originalOutDir := defaultCaddyCertOutDir
	defer func() {
		findCaddyCertPair = originalFinder
		defaultCaddyCertOutDir = originalOutDir
	}()

	root := t.TempDir()
	defaultCaddyCertOutDir = root
	certPath := filepath.Join(root, "example.com.crt")
	keyPath := filepath.Join(root, "example.com.key")
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: certPath, KeyPath: keyPath}, nil
	}

	result, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com"}, ProductionConfig{})
	if err != nil {
		t.Fatalf("sync caddy cert: %v", err)
	}
	if !strings.HasPrefix(result.CertPath, root) {
		t.Fatalf("default cert path = %q, want prefix %q", result.CertPath, root)
	}
}

func TestRunSyncCaddyCertReturnsNotFound(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}

	result, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "missing.example.com", OutDir: t.TempDir()}, ProductionConfig{})
	if err != nil {
		t.Fatalf("expected no error for missing cert, got %v", err)
	}
	if result.Found {
		t.Fatal("expected Found=false")
	}
}

func TestRunSyncCaddyCertPropagatesCertReadError(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: "/does/not/exist.crt", KeyPath: "/does/not/exist.key"}, nil
	}

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: t.TempDir()}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected cert read error")
	}
}

func TestRunSyncCaddyCertPropagatesKeyReadError(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	root := t.TempDir()
	certPath := filepath.Join(root, "cert.crt")
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: certPath, KeyPath: "/does/not/exist.key"}, nil
	}

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: t.TempDir()}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected key read error")
	}
}

func TestRunSyncCaddyCertFailsToCreateOutDir(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	root := t.TempDir()
	certPath := filepath.Join(root, "cert.crt")
	keyPath := filepath.Join(root, "cert.key")
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: certPath, KeyPath: keyPath}, nil
	}

	// Make OutDir a file so MkdirAll fails.
	outDir := filepath.Join(root, "out")
	if err := os.WriteFile(outDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: outDir}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestFindCaddyCertWithRetryFastPath(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	findCaddyCertPair = func(_, domain string) (caddycert.Pair, error) {
		if domain == "fast.example.com" {
			return caddycert.Pair{CertPath: "/tmp/cert.crt", KeyPath: "/tmp/cert.key"}, nil
		}
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}

	pair, err := findCaddyCertWithRetry(context.Background(), "fast.example.com")
	if err != nil {
		t.Fatalf("fast path: %v", err)
	}
	if pair.CertPath != "/tmp/cert.crt" {
		t.Fatalf("unexpected pair: %+v", pair)
	}
}

func TestFindCaddyCertWithRetryPollsUntilFound(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	calls := 0
	findCaddyCertPair = func(_, domain string) (caddycert.Pair, error) {
		calls++
		if calls < 2 {
			return caddycert.Pair{}, caddycert.ErrCertificateNotFound
		}
		return caddycert.Pair{CertPath: "/tmp/cert.crt", KeyPath: "/tmp/cert.key"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pair, err := findCaddyCertWithRetry(ctx, "poll.example.com")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if pair.CertPath != "/tmp/cert.crt" {
		t.Fatalf("unexpected pair: %+v", pair)
	}
	if calls < 2 {
		t.Fatalf("expected retries, got %d calls", calls)
	}
}

func TestFindCaddyCertWithRetryRespectsContextCancellation(t *testing.T) {
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := findCaddyCertWithRetry(ctx, "cancelled.example.com")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestFindCaddyCertWithRetryTimesOutWithoutDeadline(t *testing.T) {
	originalFinder := findCaddyCertPair
	originalInterval := caddyRetryInterval
	defer func() {
		findCaddyCertPair = originalFinder
		caddyRetryInterval = originalInterval
	}()

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}
	caddyRetryInterval = 10 * time.Millisecond

	_, err := findCaddyCertWithRetry(context.Background(), "timeout.example.com")
	if !errors.Is(err, caddycert.ErrCertificateNotFound) {
		t.Fatalf("expected ErrCertificateNotFound, got %v", err)
	}
}
