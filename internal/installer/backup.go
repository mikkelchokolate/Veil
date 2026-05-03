package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
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
	backupID = time.Now().UTC().Format("20060102_150405")

	backupPath := filepath.Join(backupDir, backupID)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	manifest := backupManifest{}

	for _, src := range paths {
		srcInfo, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", src, err)
		}
		if srcInfo.IsDir() {
			continue
		}

		dst := filepath.Join(backupPath, filepath.Base(src))

		// Copy file contents
		if err := copyFile(src, dst, srcInfo.Mode()); err != nil {
			return "", fmt.Errorf("backup %s: %w", src, err)
		}

		manifest.Entries = append(manifest.Entries, BackupEntry{
			OriginalPath: src,
			BackupPath:   dst,
			Size:         srcInfo.Size(),
		})
	}

	// Write manifest
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(backupPath, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}

	return backupID, nil
}

// RestoreFromBackup restores files from a backup directory to their original locations.
// Returns the list of restored original paths.
func RestoreFromBackup(backupDir string, backupID string) ([]string, error) {
	backupPath := filepath.Join(backupDir, backupID)
	info, err := os.Stat(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("backup %s does not exist in %s", backupID, backupDir)
		}
		return nil, fmt.Errorf("stat backup dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("backup %s is not a directory", backupID)
	}

	// Read manifest
	manifestPath := filepath.Join(backupPath, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest not found in backup %s", backupID)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest backupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	var restored []string
	for _, entry := range manifest.Entries {
		// Ensure parent directory of original path exists
		parentDir := filepath.Dir(entry.OriginalPath)
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return nil, fmt.Errorf("create parent dir %s: %w", parentDir, err)
		}

		// Get file info for permissions from backup file
		backupFileInfo, err := os.Stat(entry.BackupPath)
		if err != nil {
			return nil, fmt.Errorf("stat backup file %s: %w", entry.BackupPath, err)
		}

		// Copy from backup to original location
		if err := copyFile(entry.BackupPath, entry.OriginalPath, backupFileInfo.Mode()); err != nil {
			return nil, fmt.Errorf("restore %s: %w", entry.OriginalPath, err)
		}

		restored = append(restored, entry.OriginalPath)
	}

	return restored, nil
}

// CleanupBackup removes the backup directory after successful apply.
func CleanupBackup(backupDir string, backupID string) error {
	backupPath := filepath.Join(backupDir, backupID)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup %s does not exist in %s", backupID, backupDir)
	}
	return os.RemoveAll(backupPath)
}

// ListBackups returns available backup IDs sorted by time (lexicographic sort matches chronological).
func ListBackups(backupDir string) ([]string, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}

	sort.Strings(ids)
	return ids, nil
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
