package installer

import (
	"fmt"
	"io"
	"os"
)

// BackupDir represents a backup directory path.
type BackupDir struct {
	Path string
}

// BackupEntry describes a single backed-up file.
type BackupEntry struct {
	OriginalPath string `json:"original_path"`
	BackupPath   string `json:"backup_path"`
	Size         int64  `json:"size"`
}

// backupManifest stores metadata about which files were backed up and their original paths.
type backupManifest struct {
	Entries []BackupEntry `json:"entries"`
}

// BackupBeforeApply copies existing files to a timestamped backup directory.
// Files that don't exist are silently skipped. Returns the backup ID.
func BackupBeforeApply(paths []string, backupDir string) (backupID string, err error) {
	return NewBackupLifecycle(backupDir).BackupExisting(paths)
}

// RestoreFromBackup restores files from a backup directory to their original locations.
// Returns the list of restored original paths.
func RestoreFromBackup(backupDir string, backupID string) ([]string, error) {
	return NewBackupLifecycle(backupDir).Restore(backupID)
}

// CleanupBackup removes the backup directory after successful apply.
func CleanupBackup(backupDir string, backupID string) error {
	return NewBackupLifecycle(backupDir).Cleanup(backupID)
}

// ListBackups returns available backup IDs sorted by time (lexicographic sort matches chronological).
func ListBackups(backupDir string) ([]string, error) {
	return NewBackupLifecycle(backupDir).List()
}

// copyFile copies a file from src to dst preserving the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	return dstFile.Sync()
}
