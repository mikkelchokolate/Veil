package backup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRestoreSpacePreflightSumsWorkspaceStagingAndSafetyOnSameFilesystem(t *testing.T) {
	root := t.TempDir()
	archiveDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(archiveDir, "backup.enc")
	state := filepath.Join(root, "state.json")
	key := filepath.Join(root, "state.key")
	database := filepath.Join(root, "veil.db")
	for path, body := range map[string]string{archive: "1234", state: "123", key: "12", database: "12345"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := backupStatfs
	defer func() { backupStatfs = old }()
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = 64*1024*1024 + 4 + 10 + 10 + 3 + 2 + 5 - 1
		return nil
	}
	err := PreflightRestoreSpace(archive, state, key, database, 10)
	if err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("expected summed same-filesystem rejection, got %v", err)
	}
}
