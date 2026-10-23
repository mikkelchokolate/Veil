//go:build linux

package backup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The space-preflight tests inject statfs results through backupStatfs, which
// only exists on Linux (backup_statfs_linux.go); elsewhere the preflight
// reports "unsupported" and there is nothing to inject. Keeping them in a
// _linux_test file lets the package's test binary compile for darwin/freebsd.

func TestPreflightChargesDecryptedInspectionWorkspace(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "backup.tar.gz.enc")
	if err := os.WriteFile(archive, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	const maxBytes = int64(10)
	const reserve = int64(64 * 1024 * 1024)
	archiveRequirement := int64(4) + 2*maxBytes + reserve

	old := backupStatfs
	t.Cleanup(func() { backupStatfs = old })
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(archiveRequirement - 1)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("verify preflight accepted space short of the decrypted-workspace need: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(archiveRequirement)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err != nil {
		t.Fatalf("verify preflight rejected exactly-sufficient space: %v", err)
	}

	// Same accounting on the restore preflight: state dir on the same
	// filesystem adds its own maxBytes + member sizes.
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")
	for path, body := range map[string][]byte{statePath: []byte("123"), keyPath: []byte("12"), databasePath: []byte("12345")} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	restoreRequirement := archiveRequirement + maxBytes + 3 + 2 + 5
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(restoreRequirement - 1)
		return nil
	}
	if err := PreflightRestoreSpace(archive, statePath, keyPath, databasePath, maxBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("restore preflight accepted space short of the decrypted-workspace need: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(restoreRequirement)
		return nil
	}
	if err := PreflightRestoreSpace(archive, statePath, keyPath, databasePath, maxBytes); err != nil {
		t.Fatalf("restore preflight rejected exactly-sufficient space: %v", err)
	}
}
