package privileged

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
)

func TestRunSyncCaddyCertCopiesPairToOutDir(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	var chowns []ownershipCall
	type chmodCall struct {
		path string
		mode os.FileMode
	}
	var chmods []chmodCall
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	defer func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	}()
	effectiveUID = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		switch name {
		case "veil":
			return &user.User{Uid: "123", Gid: "456"}, nil
		case "veil-proxy":
			return &user.User{Uid: "124", Gid: "457"}, nil
		default:
			t.Fatalf("lookup user = %q, want veil or veil-proxy", name)
			return nil, nil
		}
	}
	chownPath = func(path string, uid, gid int) error {
		chowns = append(chowns, ownershipCall{path: path, uid: uid, gid: gid})
		return nil
	}
	chmodPath = func(path string, mode os.FileMode) error {
		chmods = append(chmods, chmodCall{path: path, mode: mode})
		return nil
	}
	hasChmod := func(path string, mode os.FileMode) bool {
		for _, call := range chmods {
			if call.path == path && call.mode == mode {
				return true
			}
		}
		return false
	}

	root := t.TempDir()
	caddyCertRoot = root
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

	// The synced cert material must be root:veil-proxy 0750/0640 so the
	// protocol units (User=veil-proxy) can read it (audit #530).
	for _, path := range []string{root, result.CertPath, result.KeyPath} {
		if !hasChown(chowns, path, 0, 457) {
			t.Fatalf("%s was not chowned root:veil-proxy: %+v", path, chowns)
		}
	}
	if !hasChmod(root, 0o750) {
		t.Fatalf("cert out dir was not chmodded 0750: %+v", chmods)
	}
	for _, path := range []string{result.CertPath, result.KeyPath} {
		if !hasChmod(path, 0o640) {
			t.Fatalf("%s was not chmodded 0640: %+v", path, chmods)
		}
	}
}

// SyncCaddyCert writes material the veil-proxy units must read; without root
// it cannot enforce ownership and must fail closed (audit #522/#530).
func TestRunSyncCaddyCertFailsClosedWithoutRoot(t *testing.T) {
	oldEffectiveUID := effectiveUID
	defer func() { effectiveUID = oldEffectiveUID }()
	effectiveUID = func() int { return 1000 }

	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()
	root := t.TempDir()
	caddyCertRoot = root
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

	if _, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: root}, ProductionConfig{}); err == nil {
		t.Fatal("expected non-root sync to fail closed")
	}
}

func TestRunSyncCaddyCertRequiresDomain(t *testing.T) {
	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected domain required error")
	}
}

func TestRunSyncCaddyCertDefaultsOutDir(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	originalFinder := findCaddyCertPair
	originalOutDir := defaultCaddyCertOutDir
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = originalFinder
		defaultCaddyCertOutDir = originalOutDir
		caddyCertRoot = originalRoot
	}()

	root := t.TempDir()
	defaultCaddyCertOutDir = root
	caddyCertRoot = root
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
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	caddyCertRoot = t.TempDir()
	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{}, caddycert.ErrCertificateNotFound
	}

	result, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "missing.example.com", OutDir: caddyCertRoot}, ProductionConfig{})
	if err != nil {
		t.Fatalf("expected no error for missing cert, got %v", err)
	}
	if result.Found {
		t.Fatal("expected Found=false")
	}
}

func TestRunSyncCaddyCertPropagatesCertReadError(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	caddyCertRoot = t.TempDir()
	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: "/does/not/exist.crt", KeyPath: "/does/not/exist.key"}, nil
	}

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: caddyCertRoot}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected cert read error")
	}
}

func TestRunSyncCaddyCertPropagatesKeyReadError(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	root := t.TempDir()
	caddyCertRoot = root
	certPath := filepath.Join(root, "cert.crt")
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}

	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: certPath, KeyPath: "/does/not/exist.key"}, nil
	}

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: caddyCertRoot}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected key read error")
	}
}

func TestRunSyncCaddyCertFailsToCreateOutDir(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	root := t.TempDir()
	caddyCertRoot = root
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

func TestRunSyncCaddyCertRejectsTraversalDomain(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	caddyCertRoot = t.TempDir()
	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "../../etc/cron.d/x", OutDir: caddyCertRoot}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected traversal domain rejection")
	}
}

func TestRunSyncCaddyCertRejectsTraversalOutDir(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	root := t.TempDir()
	caddyCertRoot = root
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

	_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: filepath.Join(root, "..", "escape")}, ProductionConfig{})
	if err == nil {
		t.Fatal("expected traversal OutDir rejection")
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

// #537: a lookup failure that is not ErrCertificateNotFound (permission,
// I/O, …) is not an ACME "still issuing" miss — it must propagate, on both
// the fast path and inside the poll loop, instead of collapsing to
// Found:false.
func TestRunSyncCaddyCertPropagatesLookupError(t *testing.T) {
	original := findCaddyCertPair
	originalRoot := caddyCertRoot
	defer func() {
		findCaddyCertPair = original
		caddyCertRoot = originalRoot
	}()

	caddyCertRoot = t.TempDir()
	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		return caddycert.Pair{}, errors.New("permission denied")
	}

	result, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com", OutDir: caddyCertRoot}, ProductionConfig{})
	if err == nil {
		t.Fatalf("expected lookup error to propagate, got Found=%v", result.Found)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v, want underlying permission cause", err)
	}
}

func TestFindCaddyCertWithRetryPropagatesNonNotFoundError(t *testing.T) {
	originalFinder := findCaddyCertPair
	originalInterval := caddyRetryInterval
	defer func() {
		findCaddyCertPair = originalFinder
		caddyRetryInterval = originalInterval
	}()
	caddyRetryInterval = time.Millisecond

	// First call is a genuine miss (enters the poll loop), second call fails
	// hard — the loop must surface it instead of retrying or reporting
	// not-found.
	calls := 0
	findCaddyCertPair = func(_, _ string) (caddycert.Pair, error) {
		calls++
		if calls == 1 {
			return caddycert.Pair{}, caddycert.ErrCertificateNotFound
		}
		return caddycert.Pair{}, errors.New("i/o error reading cert dir")
	}

	_, err := findCaddyCertWithRetry(context.Background(), "flaky.example.com")
	if err == nil || errors.Is(err, caddycert.ErrCertificateNotFound) {
		t.Fatalf("expected the I/O error to propagate, got %v", err)
	}
	if !strings.Contains(err.Error(), "i/o error") {
		t.Fatalf("error = %v, want underlying cause", err)
	}
}
