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
