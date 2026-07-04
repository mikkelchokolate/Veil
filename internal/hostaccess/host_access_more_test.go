package hostaccess

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPreparePropagatesEnsureAccountError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX account test")
	}
	original := testHooks.prepareAccountDeps
	defer func() { testHooks.prepareAccountDeps = original }()
	testHooks.prepareAccountDeps = func() AccountDependencies {
		return AccountDependencies{
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("no user") },
			LookupGroup: func(string) (*user.Group, error) { return nil, errors.New("no group") },
			Run:         func(string, ...string) error { return errors.New("no command") },
		}
	}

	err := Prepare(Paths{EtcDir: t.TempDir(), VarDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error from Prepare")
	}
}

func TestMigrateSafetyCopyOwnershipError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	key := filepath.Join(etcDir, "state.key")
	if err := os.MkdirAll(filepath.Dir(key), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	uid, gid := os.Getuid(), os.Getgid()
	safetyRoot := filepath.Join(varDir, "migration-backups", now.UTC().Format("20060102T150405Z"))

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(path string, mode os.FileMode) error {
		if path == safetyRoot {
			return errors.New("chmod safety root failed")
		}
		return originalChmod(path, mode)
	}

	err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid},
		func() time.Time { return now },
	)
	if err == nil || !strings.Contains(err.Error(), "chmod safety root failed") {
		t.Fatalf("expected chmod safety root error, got: %v", err)
	}
}

func TestMigrateVarOptionalFileChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	stateJSON := filepath.Join(varDir, "state.json")

	for _, path := range []string{
		filepath.Join(etcDir, "generated"),
		filepath.Join(etcDir, "tls"),
		filepath.Join(etcDir, "panel"),
		varDir,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(stateJSON, []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(path string, mode os.FileMode) error {
		if path == stateJSON {
			return errors.New("chmod state.json failed")
		}
		return originalChmod(path, mode)
	}

	uid, gid := os.Getuid(), os.Getgid()
	err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid},
		time.Now,
	)
	if err == nil || !strings.Contains(err.Error(), "chmod state.json failed") {
		t.Fatalf("expected chmod state.json error, got: %v", err)
	}
}

func TestMigrateEtcOptionalFileChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	stateKey := filepath.Join(etcDir, "state.key")

	for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
		if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"generated", "tls", "panel"} {
		if err := os.MkdirAll(filepath.Join(etcDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(stateKey, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state.json", "sessions.json"} {
		if err := os.WriteFile(filepath.Join(varDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(path string, mode os.FileMode) error {
		if path == stateKey {
			return errors.New("chmod state.key failed")
		}
		return originalChmod(path, mode)
	}

	uid, gid := os.Getuid(), os.Getgid()
	err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid},
		time.Now,
	)
	if err == nil || !strings.Contains(err.Error(), "chmod state.key failed") {
		t.Fatalf("expected chmod state.key error, got: %v", err)
	}
}

func TestCreateSafetyCopiesLstatError(t *testing.T) {
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	originalLstat := testHooks.lstat
	defer func() { testHooks.lstat = originalLstat }()
	callCount := 0
	testHooks.lstat = func(path string) (os.FileInfo, error) {
		callCount++
		if path == filepath.Join(etcDir, "state.key") {
			return nil, errors.New("lstat failed")
		}
		return originalLstat(path)
	}

	_, err := createSafetyCopies(Paths{EtcDir: etcDir, VarDir: varDir}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "lstat failed") {
		t.Fatalf("expected lstat error, got: %v", err)
	}
	if callCount == 0 {
		t.Fatal("expected lstat to be called")
	}
}

func TestCreateSafetyCopiesCopyError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	key := filepath.Join(etcDir, "state.key")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalCopy := testHooks.copy
	defer func() { testHooks.copy = originalCopy }()
	testHooks.copy = func(dst io.Writer, src io.Reader) (int64, error) {
		return 0, errors.New("copy failed")
	}

	_, err := createSafetyCopies(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("expected copy error, got: %v", err)
	}
}

func TestCopyRegularFileCopyError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalCopy := testHooks.copy
	defer func() { testHooks.copy = originalCopy }()
	testHooks.copy = func(dst io.Writer, src io.Reader) (int64, error) {
		return 0, errors.New("copy failed")
	}

	err := copyRegularFile(src, filepath.Join(t.TempDir(), "dst"))
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("expected copy error, got: %v", err)
	}
}

func TestEnsureOwnedDirectoryChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	target := filepath.Join(t.TempDir(), "target")

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(path string, mode os.FileMode) error {
		if path == target {
			return errors.New("chmod failed")
		}
		return originalChmod(path, mode)
	}

	err := ensureOwnedDirectory(target, 0o700, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "chmod failed") {
		t.Fatalf("expected chmod error, got: %v", err)
	}
}

func TestApplyTreeOwnershipWalkError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}

	originalWalkDir := testHooks.walkDir
	defer func() { testHooks.walkDir = originalWalkDir }()
	testHooks.walkDir = func(path string, fn fs.WalkDirFunc) error {
		return fn(path, nil, errors.New("walk failed"))
	}

	err := applyTreeOwnership(tree, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "walk failed") {
		t.Fatalf("expected walk error, got: %v", err)
	}
}

func TestApplyTreeOwnershipEntryInfoError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}

	originalWalkDir := testHooks.walkDir
	defer func() { testHooks.walkDir = originalWalkDir }()
	testHooks.walkDir = func(path string, fn fs.WalkDirFunc) error {
		return fn(filepath.Join(path, "x"), failingInfoEntry{name: "x"}, nil)
	}

	err := applyTreeOwnership(tree, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "info error") {
		t.Fatalf("expected info error, got: %v", err)
	}
}

func TestApplyTreeOwnershipChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	file := filepath.Join(tree, "file")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(path string, mode os.FileMode) error {
		if path == file {
			return errors.New("chmod file failed")
		}
		return originalChmod(path, mode)
	}

	err := applyTreeOwnership(tree, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "chmod file failed") {
		t.Fatalf("expected chmod file error, got: %v", err)
	}
}

func TestSetOptionalFileLstatError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")

	originalLstat := testHooks.lstat
	defer func() { testHooks.lstat = originalLstat }()
	testHooks.lstat = func(p string) (os.FileInfo, error) {
		if p == path {
			return nil, errors.New("lstat failed")
		}
		return originalLstat(p)
	}

	err := setOptionalFile(path, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "lstat failed") {
		t.Fatalf("expected lstat error, got: %v", err)
	}
}

func TestSetOptionalFileChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalChmod := testHooks.chmod
	defer func() { testHooks.chmod = originalChmod }()
	testHooks.chmod = func(p string, mode os.FileMode) error {
		if p == path {
			return errors.New("chmod failed")
		}
		return originalChmod(p, mode)
	}

	err := setOptionalFile(path, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "chmod failed") {
		t.Fatalf("expected chmod error, got: %v", err)
	}
}

// failingInfoEntry is a DirEntry whose Info() method always returns an error.
type failingInfoEntry struct {
	name string
}

func (e failingInfoEntry) Name() string               { return e.name }
func (e failingInfoEntry) IsDir() bool                { return false }
func (e failingInfoEntry) Type() fs.FileMode          { return 0 }
func (e failingInfoEntry) Info() (fs.FileInfo, error) { return nil, errors.New("info error") }
