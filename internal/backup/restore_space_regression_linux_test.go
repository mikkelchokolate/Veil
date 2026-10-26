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
	// The inspection workspace is charged twice the archive's real size
	// (#1254 F2); on the same filesystem the state directory adds its own
	// maxBytes plus the staged member sizes.
	const reserve = int64(64 * 1024 * 1024)
	const required = 2*4 + 10 + 3 + 2 + 5
	old := backupStatfs
	defer func() { backupStatfs = old }()
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(required + reserve - 1)
		return nil
	}
	err := PreflightRestoreSpace(archive, state, key, database, 10)
	if err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("expected summed same-filesystem rejection, got %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(required + reserve)
		return nil
	}
	if err := PreflightRestoreSpace(archive, state, key, database, 10); err != nil {
		t.Fatalf("restore preflight rejected exactly-sufficient space: %v", err)
	}
}
