package backup

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestBackupSpacePreflightRejectsBeforeWorkspaceCreation(t *testing.T) {
	dir := t.TempDir()
	sources := make([]string, 3)
	for i := range sources {
		file, err := os.CreateTemp(dir, "source-*")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(make([]byte, 1024)); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		sources[i] = file.Name()
	}
	old := backupStatfs
	backupStatfs = func(string, *syscall.Statfs_t) error { return nil }
	t.Cleanup(func() { backupStatfs = old })
	err := preflightBackupSpace(dir, sources, 1024*1024)
	if err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("low-space preflight error=%v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".veil-backup-") {
			t.Fatalf("workspace created before preflight: %s", entry.Name())
		}
	}
}

// TestBackupSpacePreflightChargesThreeCopiesOnOneFilesystem locks the create
// formula from #1254: the destination holds the pending archive and the final
// link while the private workspace holds the decrypted tarball and the extracted
// members, so a shared filesystem needs three copies of the sources plus the
// reserve — not four (double-charging both directories) and not two.
func TestBackupSpacePreflightChargesThreeCopiesOnOneFilesystem(t *testing.T) {
	dir := t.TempDir()
	sources := make([]string, 3)
	for i := range sources {
		file, err := os.CreateTemp(dir, "source-*")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(make([]byte, 1024)); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		sources[i] = file.Name()
	}
	const reserve = int64(64 * 1024 * 1024)
	const required = 3*3*1024 + reserve

	old := backupStatfs
	t.Cleanup(func() { backupStatfs = old })
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(required - 1)
		return nil
	}
	if err := preflightBackupSpace(dir, sources, 1024*1024); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("preflight accepted one byte short of three copies: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(required)
		return nil
	}
	if err := preflightBackupSpace(dir, sources, 1024*1024); err != nil {
		t.Fatalf("preflight rejected exactly three copies: %v", err)
	}
}
