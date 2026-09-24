package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRetentionPolicy(t *testing.T) {
	p := DefaultRetentionPolicy()
	if p.Daily != 7 || p.Weekly != 4 || p.Monthly != 12 {
		t.Fatalf("unexpected default policy: %+v", p)
	}
}

func TestListArchivesReadDirError(t *testing.T) {
	dir := t.TempDir()
	// Use a file path as the archive dir; ReadDir returns ENOTDIR.
	path := filepath.Join(dir, "notadir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListArchives(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestPruneArchivesNegativeRetention(t *testing.T) {
	if _, err := PruneArchives(t.TempDir(), RetentionPolicy{Daily: -1}, false); err == nil {
		t.Fatal("expected error for negative retention")
	}
}

func TestListArchivesMissingDir(t *testing.T) {
	archives, err := ListArchives(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected empty result, got %v", archives)
	}
}

func TestListArchivesSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	archives, err := ListArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected no archives, got %v", archives)
	}
}

func TestListArchivesSortTie(t *testing.T) {
	dir := t.TempDir()
	base := "veil_backup_20260101_120000"
	if err := os.WriteFile(filepath.Join(dir, base+".tar.gz"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, base+".tar.gz.enc"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	archives, err := ListArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 2 {
		t.Fatalf("expected 2 archives, got %d", len(archives))
	}
	if archives[0].Name != base+".tar.gz.enc" {
		t.Fatalf("expected encrypted archive first, got %s", archives[0].Name)
	}
}

func TestListArchivesEntryInfoError(t *testing.T) {
	dir := t.TempDir()
	name := "veil_backup_20260101_120000.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := retentionEntryInfo
	defer func() { retentionEntryInfo = orig }()
	retentionEntryInfo = func(os.DirEntry) (os.FileInfo, error) {
		return nil, errors.New("injected info error")
	}

	if _, err := ListArchives(dir); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseArchiveTimestampInvalidDate(t *testing.T) {
	name := "veil_backup_20269999_999999.tar.gz"
	if ts, ok := parseArchiveTimestamp(name); ok {
		t.Fatalf("expected parse failure, got %v", ts)
	}
}

func TestPruneArchivesListError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notadir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneArchives(path, DefaultRetentionPolicy(), false); err == nil {
		t.Fatal("expected error")
	}
}

func TestPruneArchivesRemoveError(t *testing.T) {
	dir := t.TempDir()
	name := "veil_backup_20260101_120000.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := retentionRemove
	defer func() { retentionRemove = orig }()
	retentionRemove = func(string) error {
		return errors.New("injected remove error")
	}

	_, err := PruneArchives(dir, RetentionPolicy{}, false)
	if err == nil || !strings.Contains(err.Error(), "injected remove error") {
		t.Fatalf("expected remove error, got %v", err)
	}
}

// Regression for #965: when a delete fails mid-prune, the archives that were
// already removed must still be reported — and the failed archive must NOT be
// counted as deleted.
func TestPruneArchivesReportsPartialDeletionOnFailure(t *testing.T) {
	dir := t.TempDir()
	// Newest-first order: the newest archive is deleted first, then the
	// removal of the middle one fails.
	names := []string{
		"veil_backup_20260301_120000.tar.gz",
		"veil_backup_20260201_120000.tar.gz",
		"veil_backup_20260101_120000.tar.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	orig := retentionRemove
	defer func() { retentionRemove = orig }()
	removals := 0
	retentionRemove = func(path string) error {
		removals++
		if removals == 2 {
			return errors.New("injected remove error")
		}
		return os.Remove(path)
	}

	result, err := PruneArchives(dir, RetentionPolicy{}, false)
	if err == nil || !strings.Contains(err.Error(), "injected remove error") {
		t.Fatalf("expected remove error, got %v", err)
	}
	if !strings.Contains(err.Error(), names[1]) {
		t.Fatalf("error should name the archive whose removal failed, got %v", err)
	}
	// Only the archive actually removed before the failure is reported deleted;
	// the failed name and everything after it are not.
	if len(result.Deleted) != 1 || result.Deleted[0] != names[0] {
		t.Fatalf("partial prune deleted = %v, want [%s]", result.Deleted, names[0])
	}
	if _, statErr := os.Stat(filepath.Join(dir, names[0])); !os.IsNotExist(statErr) {
		t.Fatalf("deleted archive %s still present (stat err=%v)", names[0], statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, names[1])); statErr != nil {
		t.Fatalf("failed archive %s should remain: %v", names[1], statErr)
	}
}
