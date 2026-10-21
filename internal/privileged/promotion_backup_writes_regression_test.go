//go:build linux

package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// Regression for #1274 (residual): a safety copy written while promoting must
// not follow a symlink planted inside the backup root — the helper's own
// writes resolve every component descriptor-relative with O_NOFOLLOW.
func TestPromotionSafetyCopyRefusesSymlinkedBackupDirectory(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 1)
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(backupRoot, fixedPromotionBackupID())); err != nil {
		t.Fatal(err)
	}
	withStubbedArtifactOwnership(t, func() {
		if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, request); err == nil {
			t.Fatal("promotion followed a symlinked backup directory")
		}
	})
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("safety copy escaped the backup root: %v", entries)
	}
	assertPromotionSet(t, request, "old")
}

func TestWritePromotionBackupMemberRefusesSymlinkedIntermediate(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "id")); err != nil {
		t.Fatal(err)
	}
	rootDir, err := safefs.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDir.Close()
	if err := writePromotionBackupMember(rootDir, root, filepath.Join(root, "id", "manifest.json"), []byte("x")); err == nil {
		t.Fatal("backup member write followed a symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest written outside the backup root: %v", err)
	}
	if err := writePromotionBackupMember(rootDir, root, filepath.Join(root, "x", "..", "..", "y"), []byte("x")); err == nil {
		t.Fatal("backup member write accepted an escaping path")
	}
}

func TestWritePromotionBackupMemberCreatesNestedDirectories(t *testing.T) {
	root := t.TempDir()
	rootDir, err := safefs.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDir.Close()
	path := filepath.Join(root, "id", "caddy", "config.json")
	if err := writePromotionBackupMember(rootDir, root, path, []byte("body")); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, path, "body")
	for _, dir := range []string{filepath.Join(root, "id"), filepath.Join(root, "id", "caddy")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode=%#o want 0700", dir, info.Mode().Perm())
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("member mode: %v %v", info, err)
	}
	// Overwrite publishes atomically over the existing leaf.
	if err := writePromotionBackupMember(rootDir, root, path, []byte("again")); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, path, "again")
}

// The journal is written and removed through the pinned root descriptor: a
// root renamed away and replaced after it was pinned receives nothing.
func TestPromotionJournalWritesStayInPinnedRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "backups")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rootDir, err := safefs.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDir.Close()
	moved := filepath.Join(base, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := promotionTransactionJournal{Version: 1, TransactionID: "tx", Phase: "prepared"}
	if err := writePromotionJournal(rootDir, journal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, promotionTransactionJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal written into the replacement root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, promotionTransactionJournalName)); err != nil {
		t.Fatalf("journal missing from pinned root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, promotionTransactionJournalName), []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removePromotionJournal(rootDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, promotionTransactionJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pinned journal not removed: %v", err)
	}
	assertFileContent(t, filepath.Join(root, promotionTransactionJournalName), "planted")
}
