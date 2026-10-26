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

// TestPreflightChargesDecryptedInspectionWorkspace covers #1119 and #1254 F2:
// an encrypted archive's inspect workspace holds the decrypted tarball AND the
// extracted members at once, so the filesystem hosting it is charged twice the
// archive's real size. The archive directory is no longer charged — reading an
// archive needs no room there.
func TestPreflightChargesDecryptedInspectionWorkspace(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "backup.tar.gz.enc")
	if err := os.WriteFile(archive, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	const maxBytes = int64(10)
	const reserve = int64(64 * 1024 * 1024)
	workspaceRequirement := 2*4 + reserve

	old := backupStatfs
	t.Cleanup(func() { backupStatfs = old })
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(workspaceRequirement - 1)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("verify preflight accepted space short of the decrypted-workspace need: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(workspaceRequirement)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err != nil {
		t.Fatalf("verify preflight rejected exactly-sufficient space: %v", err)
	}

	// The restore preflight adds the state directory on the same filesystem:
	// its own maxBytes plus the staged member sizes.
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")
	for path, body := range map[string][]byte{statePath: []byte("123"), keyPath: []byte("12"), databasePath: []byte("12345")} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	restoreRequirement := workspaceRequirement + maxBytes + 3 + 2 + 5
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

// TestPreflightVerifySpaceChargesTheArchiveSizeNotThePolicy keeps a real verify
// possible on the privileged helper's PrivateTmp tmpfs: the inspection
// workspace is charged by the archive's size, not by the 16 GiB default policy,
// which demanded tens of gigabytes of RAM per verify (#1254 review).
func TestPreflightVerifySpaceChargesTheArchiveSizeNotThePolicy(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "backup.tar.gz.enc")
	if err := os.WriteFile(archive, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	const reserve = int64(64 * 1024 * 1024)
	old := backupStatfs
	t.Cleanup(func() { backupStatfs = old })
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(2*4 + reserve)
		return nil
	}
	if err := PreflightVerifySpace(archive, DefaultMaxBackupBytes); err != nil {
		t.Fatalf("verify preflight demanded more than the archive needs: %v", err)
	}
}
