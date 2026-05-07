package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type BackupLifecycle struct {
	Dir string
}

func NewBackupLifecycle(dir string) BackupLifecycle {
	return BackupLifecycle{Dir: dir}
}

func (l BackupLifecycle) BackupExisting(paths []string) (string, error) {
	backupID, err := NewBackupIDPolicy(time.Now, backupPathExists).Next(l.Dir)
	if err != nil {
		return "", err
	}

	backupPath := filepath.Join(l.Dir, backupID)
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

	manifestPath := filepath.Join(backupPath, "manifest.json")
	if err := NewBackupManifestStore(manifestPath).Save(manifest); err != nil {
		return "", err
	}

	return backupID, nil
}

func (l BackupLifecycle) Restore(backupID string) ([]string, error) {
	backupPath := filepath.Join(l.Dir, backupID)
	info, err := os.Stat(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return nil, fmt.Errorf("stat backup dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("backup %s is not a directory", backupID)
	}

	manifestPath := filepath.Join(backupPath, "manifest.json")
	manifest, err := NewBackupManifestStore(manifestPath).Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest not found in backup %s", backupID)
		}
		return nil, err
	}

	// Collect existing original files to create a safety backup before overwriting
	var existingPaths []string
	for _, entry := range manifest.Entries {
		if _, err := os.Stat(entry.OriginalPath); err == nil {
			existingPaths = append(existingPaths, entry.OriginalPath)
		}
	}
	if len(existingPaths) > 0 {
		if _, safetyErr := l.BackupExisting(existingPaths); safetyErr != nil {
			return nil, fmt.Errorf("create safety backup before restore: %w", safetyErr)
		}
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

func (l BackupLifecycle) Cleanup(backupID string) error {
	backupPath := filepath.Join(l.Dir, backupID)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
	}
	return os.RemoveAll(backupPath)
}

func (l BackupLifecycle) List() ([]string, error) {
	entries, err := os.ReadDir(l.Dir)
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
