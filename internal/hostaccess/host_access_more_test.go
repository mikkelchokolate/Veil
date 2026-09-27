package hostaccess

import (
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
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

	originalChmodDir := testHooks.chmodDir
	defer func() { testHooks.chmodDir = originalChmodDir }()
	testHooks.chmodDir = func(d *safefs.Dir, mode os.FileMode) error {
		if d.Path() == safetyRoot {
			return errors.New("chmod safety root failed")
		}
		return originalChmodDir(d, mode)
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

	dir, err := createSafetyCopies(Paths{EtcDir: etcDir, VarDir: varDir}, time.Now())
	if dir != nil {
		_ = dir.Close()
	}
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

	dir, err := createSafetyCopies(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}, time.Now())
	if dir != nil {
		_ = dir.Close()
	}
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

	originalWalk := walkManagedDirHook
	defer func() { walkManagedDirHook = originalWalk }()
	walkManagedDirHook = func(dir *safefs.Dir, visit func(*managedEntry) error) error {
		return errors.New("walk failed")
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
	if err := os.WriteFile(filepath.Join(tree, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalStat := testHooks.statEntryAt
	defer func() { testHooks.statEntryAt = originalStat }()
	testHooks.statEntryAt = func(dir *safefs.Dir, name string) (os.FileInfo, error) {
		return nil, errors.New("info error")
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

	originalChmod := testHooks.chmodEntryAt
	defer func() { testHooks.chmodEntryAt = originalChmod }()
	testHooks.chmodEntryAt = func(e *managedEntry, mode os.FileMode) error {
		if e.Path() == file {
			return errors.New("chmod file failed")
		}
		return originalChmod(e, mode)
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

// Issue #623: veil-caddy.service declares StateDirectory=caddy and runs as
// veil-proxy, but systemd never re-owns an existing state dir — an old
// veil-owned /var/lib/caddy stays unwritable to the unit. Migrate must
// re-own every tree in proxyStateDirs to the proxy identity, mirroring the
// package postinstall chown -R repair. (The packaged default is
// /var/lib/caddy only — the mita StateDirectory belongs to the dedicated
// veil-mita identity since #624; this test injects a scratch list purely to
// exercise the re-own mechanism, see #660.)
func TestMigrateReownsProxyStateDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	for _, dir := range []string{etcDir, varDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	caddyDir := filepath.Join(root, "var", "lib", "caddy")
	mitaDir := filepath.Join(root, "var", "lib", "mita")
	mitaFile := filepath.Join(mitaDir, "nested", "session.pb")
	for _, dir := range []string{caddyDir, filepath.Dir(mitaFile)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mitaFile, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalDirs := proxyStateDirs
	defer func() { proxyStateDirs = originalDirs }()
	proxyStateDirs = []string{caddyDir, mitaDir}

	type chownCall struct {
		path     string
		uid, gid int
	}
	var chowned []chownCall
	originalChown := testHooks.chown
	originalChownEntry := testHooks.chownEntryAt
	defer func() { testHooks.chown, testHooks.chownEntryAt = originalChown, originalChownEntry }()
	testHooks.chown = func(path string, uid, gid int) error {
		chowned = append(chowned, chownCall{path, uid, gid})
		return nil
	}
	testHooks.chownEntryAt = func(e *managedEntry, uid, gid int) error {
		chowned = append(chowned, chownCall{e.Path(), uid, gid})
		return nil
	}

	uid, gid := os.Getuid(), os.Getgid()
	err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid, ProxyUID: 4242, ProxyGID: 4343},
		time.Now,
	)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, want := range []string{caddyDir, mitaDir, filepath.Dir(mitaFile), mitaFile} {
		found := false
		for _, call := range chowned {
			if call.path == want && call.uid == 4242 && call.gid == 4343 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected proxy re-own of %s to 4242:4343, chowned=%+v", want, chowned)
		}
	}
}

// Issue #623: a symlinked or absent state dir is operator-managed — Migrate
// leaves it alone rather than chowning through the link into foreign trees.
func TestMigrateSkipsSymlinkedProxyStateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	for _, dir := range []string{etcDir, varDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	realDir := filepath.Join(root, "real-mita")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "var", "lib", "mita")
	if err := os.MkdirAll(filepath.Dir(linkDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}

	originalDirs := proxyStateDirs
	defer func() { proxyStateDirs = originalDirs }()
	proxyStateDirs = []string{linkDir}

	var chowned []string
	originalChown := testHooks.chown
	defer func() { testHooks.chown = originalChown }()
	testHooks.chown = func(path string, uid, gid int) error {
		chowned = append(chowned, path)
		return nil
	}

	uid, gid := os.Getuid(), os.Getgid()
	if err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid, ProxyUID: 4242, ProxyGID: 4343},
		time.Now,
	); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, path := range chowned {
		if path == linkDir || strings.HasPrefix(path, realDir) {
			t.Fatalf("must not chown through symlinked state dir: %s", path)
		}
	}
}

// Issue #623: a failed chown inside a proxy state dir must fail Migrate —
// leaving half the tree unwritable while reporting success would be worse
// than aborting.
func TestMigrateProxyStateDirChownErrorFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	for _, dir := range []string{etcDir, varDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mitaDir := filepath.Join(root, "var", "lib", "mita")
	if err := os.MkdirAll(mitaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	originalDirs := proxyStateDirs
	defer func() { proxyStateDirs = originalDirs }()
	proxyStateDirs = []string{mitaDir}

	originalChown := testHooks.chown
	defer func() { testHooks.chown = originalChown }()
	testHooks.chown = func(path string, uid, gid int) error {
		if path == mitaDir {
			return errors.New("chown mita failed")
		}
		return nil
	}

	uid, gid := os.Getuid(), os.Getgid()
	err := Migrate(
		Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid},
		Identity{UID: uid, GID: gid, ProxyUID: 4242, ProxyGID: 4343},
		time.Now,
	)
	if err == nil || !strings.Contains(err.Error(), "chown mita failed") {
		t.Fatalf("expected chown mita error, got: %v", err)
	}
}
