package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDeleteArchiveRemovesManagedNameOnly(t *testing.T) {
	dir := t.TempDir()
	name := "veil_backup_20260605_020000.tar.gz.enc"
	writeArchivePlaceholder(t, dir, name)
	if err := DeleteArchive(dir, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatalf("archive still present: %v", err)
	}
	if err := DeleteArchive(dir, "../escape.enc"); err == nil {
		t.Fatal("expected reject traversal")
	}
	if err := DeleteArchive(dir, "notes.txt"); err == nil {
		t.Fatal("expected reject unmanaged name")
	}
}

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

func TestResolveRetentionPolicyAllZeroMeansDefault(t *testing.T) {
	policy, defaulted, err := ResolveRetentionPolicy(0, 0, 0)
	if err != nil || !defaulted || policy != DefaultRetentionPolicy() {
		t.Fatalf("all-zero: policy=%+v defaulted=%t err=%v", policy, defaulted, err)
	}
	policy, defaulted, err = ResolveRetentionPolicy(0, 0, 1)
	if err != nil || defaulted || policy != (RetentionPolicy{Monthly: 1}) {
		t.Fatalf("explicit: policy=%+v defaulted=%t err=%v", policy, defaulted, err)
	}
	policy, defaulted, err = ResolveRetentionPolicy(MaxRetentionDaily, MaxRetentionWeekly, MaxRetentionMonthly)
	if err != nil || defaulted || policy != (RetentionPolicy{Daily: 365, Weekly: 104, Monthly: 120}) {
		t.Fatalf("upper bounds: policy=%+v defaulted=%t err=%v", policy, defaulted, err)
	}
	for _, tc := range []struct {
		daily, weekly, monthly int
		want                   string
	}{
		{-1, 0, 0, "daily retention must be between 0 and 365"},
		{366, 0, 0, "daily retention must be between 0 and 365"},
		{0, -1, 0, "weekly retention must be between 0 and 104"},
		{0, 105, 0, "weekly retention must be between 0 and 104"},
		{0, 0, -1, "monthly retention must be between 0 and 120"},
		{0, 0, 121, "monthly retention must be between 0 and 120"},
	} {
		if _, _, err := ResolveRetentionPolicy(tc.daily, tc.weekly, tc.monthly); err == nil || err.Error() != tc.want {
			t.Fatalf("ResolveRetentionPolicy(%d,%d,%d) err=%v, want %q", tc.daily, tc.weekly, tc.monthly, err, tc.want)
		}
	}
}
