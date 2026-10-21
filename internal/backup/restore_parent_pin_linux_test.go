//go:build linux

package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// #1253: a manifest OriginalPath that traverses a veil-planted intermediate
// symlink passes the managed-roots allowlist lexically. The restore must pin
// the destination parent by walking descriptor-relative from the configured
// root, so the symlinked component fails closed instead of redirecting a
// root-run write (or a MkdirAll) to an attacker-chosen directory.

func restoreParentPinFixture(t *testing.T, rel string) (lc Lifecycle, managed, outside, dest, id string) {
	t.Helper()
	base := t.TempDir()
	managed = filepath.Join(base, "managed")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{managed, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupsDir := filepath.Join(managed, "backups")
	id = "20240101_120000"
	backupPath := filepath.Join(backupsDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "0_member"), []byte("attacker-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest = filepath.Join(managed, filepath.FromSlash(rel))
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: dest,
		BackupPath:   "0_member",
		Size:         int64(len("attacker-bytes")),
	}}})
	lc = Lifecycle{Dir: backupsDir, RestoreRoots: []string{managed}}
	return lc, managed, outside, dest, id
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("restore escaped through an intermediate symlink into %s: %v", dir, names)
	}
}

// The exact shape from the issue: link -> /etc, dest link/sudoers.d/zz. The
// parent leaf (sudoers.d) is a real directory; only the intermediate is a
// symlink, so a leaf-only O_NOFOLLOW open of the parent does not catch it.
func TestRestoreRefusesIntermediateSymlinkInDestinationParent(t *testing.T) {
	lc, managed, outside, _, id := restoreParentPinFixture(t, "link/sudoers.d/zz")
	if err := os.MkdirAll(filepath.Join(outside, "sudoers.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(managed, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := lc.Restore(id); err == nil {
		t.Fatal("restore followed an intermediate symlink in the destination parent")
	}
	assertDirEmpty(t, filepath.Join(outside, "sudoers.d"))
}

func TestRestoreRefusesToCreateParentsThroughIntermediateSymlink(t *testing.T) {
	lc, managed, outside, _, id := restoreParentPinFixture(t, "link/new/deeper/zz")
	if err := os.Symlink(outside, filepath.Join(managed, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := lc.Restore(id); err == nil {
		t.Fatal("restore created parents through an intermediate symlink")
	}
	assertDirEmpty(t, outside)
}

func TestRestoreRefusesDeepIntermediateSymlinkInDestinationParent(t *testing.T) {
	lc, managed, outside, _, id := restoreParentPinFixture(t, "real/link/sub/zz")
	if err := os.MkdirAll(filepath.Join(managed, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(managed, "real", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := lc.Restore(id); err == nil {
		t.Fatal("restore followed a nested intermediate symlink")
	}
	assertDirEmpty(t, filepath.Join(outside, "sub"))
}

func TestRestoreCreatesMissingParentsUnderPinnedRoot(t *testing.T) {
	lc, managed, _, dest, id := restoreParentPinFixture(t, "a/b/zz")
	restored, err := lc.Restore(id)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(restored) != 1 || restored[0] != dest {
		t.Fatalf("restored = %v, want [%s]", restored, dest)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "attacker-bytes" {
		t.Fatalf("restored body = %q", body)
	}
	for _, dir := range []string{filepath.Join(managed, "a"), filepath.Join(managed, "a", "b")} {
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s is not a real directory", dir)
		}
	}
}

// The configured root itself is operator-chosen and may legitimately be
// reached through a symlink; only components below it are pinned no-follow.
func TestRestoreFollowsSymlinkedConfiguredRoot(t *testing.T) {
	lc, managed, _, _, id := restoreParentPinFixture(t, "x/zz")
	alias := filepath.Join(filepath.Dir(managed), "managed-alias")
	if err := os.Symlink(managed, alias); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(lc.Dir, id)
	dest := filepath.Join(alias, "x", "zz")
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: dest,
		BackupPath:   "0_member",
		Size:         int64(len("attacker-bytes")),
	}}})
	lc.RestoreRoots = []string{alias}
	if _, err := lc.Restore(id); err != nil {
		t.Fatalf("Restore through a symlinked configured root: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(managed, "x", "zz")); err != nil || string(body) != "attacker-bytes" {
		t.Fatalf("restored body = %q, err = %v", body, err)
	}
}
