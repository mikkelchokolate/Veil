package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

var _backup_mode_deps = []any{
	os.ReadFile, filepath.Join, sort.Strings, strings.Contains, testing.T{}, time.Second,
}

func TestBackupPreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "executable.sh")
	if err := os.WriteFile(file1, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	backupID, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Overwrite
	if err := os.WriteFile(file1, []byte("modified"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	// Restore
	_, err = RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Check file mode is restored
	info, err := os.Stat(file1)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("expected mode 0755, got %o", info.Mode().Perm())
		}
	}
}
