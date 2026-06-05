package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListArchivesIgnoresUnrelatedFilesAndSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeArchivePlaceholder(t, dir, "veil_backup_20260604_020000.tar.gz.enc")
	writeArchivePlaceholder(t, dir, "veil_backup_20260605_020000.tar.gz.enc")
	writeArchivePlaceholder(t, dir, "notes.txt")

	entries, err := ListArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 ||
		entries[0].Name != "veil_backup_20260605_020000.tar.gz.enc" ||
		entries[1].Name != "veil_backup_20260604_020000.tar.gz.enc" {
		t.Fatalf("archives = %+v", entries)
	}
}

func TestPruneArchivesAppliesDailyWeeklyMonthlyUnion(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"veil_backup_20260605_020000.tar.gz.enc",
		"veil_backup_20260604_020000.tar.gz.enc",
		"veil_backup_20260530_020000.tar.gz.enc",
		"veil_backup_20260501_020000.tar.gz.enc",
		"veil_backup_20260401_020000.tar.gz.enc",
	}
	for _, name := range names {
		writeArchivePlaceholder(t, dir, name)
	}
	writeArchivePlaceholder(t, dir, "keep-me.txt")

	result, err := PruneArchives(dir, RetentionPolicy{Daily: 2, Weekly: 1, Monthly: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	wantKept := []string{
		"veil_backup_20260604_020000.tar.gz.enc",
		"veil_backup_20260605_020000.tar.gz.enc",
	}
	slices.Sort(result.Kept)
	if !slices.Equal(result.Kept, wantKept) {
		t.Fatalf("kept=%v want=%v", result.Kept, wantKept)
	}
	wantDeleted := []string{
		"veil_backup_20260401_020000.tar.gz.enc",
		"veil_backup_20260501_020000.tar.gz.enc",
		"veil_backup_20260530_020000.tar.gz.enc",
	}
	slices.Sort(result.Deleted)
	if !slices.Equal(result.Deleted, wantDeleted) {
		t.Fatalf("deleted=%v want=%v", result.Deleted, wantDeleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep-me.txt")); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

func TestPruneArchivesDryRunDoesNotRemoveFiles(t *testing.T) {
	dir := t.TempDir()
	name := "veil_backup_20260101_020000.tar.gz.enc"
	writeArchivePlaceholder(t, dir, name)
	result, err := PruneArchives(dir, RetentionPolicy{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != name {
		t.Fatalf("dry-run result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("dry-run removed archive: %v", err)
	}
}

func writeArchivePlaceholder(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(path, timestamp, timestamp)
}
