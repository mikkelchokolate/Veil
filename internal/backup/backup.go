package backup

import "os"

// Dir represents a backup directory path.
type Dir struct {
	Path string
}

type BackupDir = Dir

// Entry describes a single backed-up file.
type Entry struct {
	OriginalPath string `json:"original_path"`
	BackupPath   string `json:"backup_path"`
	Size         int64  `json:"size"`
}

type BackupEntry = Entry

// Manifest stores metadata about which files were backed up and their original paths.
type Manifest struct {
	Entries []Entry `json:"entries"`
}

type backupManifest = Manifest

// BackupBeforeApply copies existing files to a timestamped backup directory.
// Files that don't exist are silently skipped. Returns the backup ID.
func BackupBeforeApply(paths []string, backupDir string) (backupID string, err error) {
	return NewLifecycle(backupDir).BackupExisting(paths)
}

// RestoreFromBackup restores files from a backup directory to their original locations.
// Returns the list of restored original paths.
func RestoreFromBackup(backupDir string, backupID string) ([]string, error) {
	return NewLifecycle(backupDir).Restore(backupID)
}

// CleanupBackup removes the backup directory after successful apply.
func CleanupBackup(backupDir string, backupID string) error {
	return NewLifecycle(backupDir).Cleanup(backupID)
}

// ListBackups returns available backup IDs sorted by time (lexicographic sort matches chronological).
func ListBackups(backupDir string) ([]string, error) {
	return NewLifecycle(backupDir).List()
}

// copyFile copies a file from src to dst preserving the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	return NewFileCopier().Copy(src, dst, mode)
}
