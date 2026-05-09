package installer

import (
	"os"
	"time"

	backup "github.com/veil-panel/veil/internal/backup"
)

type BackupDir = backup.Dir
type BackupEntry = backup.Entry
type backupManifest = backup.Manifest
type BackupLifecycle = backup.Lifecycle
type BackupIDPolicy = backup.IDPolicy
type BackupManifestStore = backup.ManifestStore
type BackupSafetyPolicy = backup.SafetyPolicy
type BackupFileCopier = backup.FileCopier

func NewBackupLifecycle(dir string) BackupLifecycle { return backup.NewLifecycle(dir) }

func NewBackupIDPolicy(now func() time.Time, exists func(path string) (bool, error)) BackupIDPolicy {
	return backup.NewIDPolicy(now, exists)
}

func NewBackupManifestStore(path string) BackupManifestStore { return backup.NewManifestStore(path) }

func NewBackupSafetyPolicy() BackupSafetyPolicy { return backup.NewSafetyPolicy() }

func NewBackupFileCopier() BackupFileCopier { return backup.NewFileCopier() }

func BackupBeforeApply(paths []string, backupDir string) (backupID string, err error) {
	return backup.BackupBeforeApply(paths, backupDir)
}

func RestoreFromBackup(backupDir string, backupID string) ([]string, error) {
	return backup.RestoreFromBackup(backupDir, backupID)
}

func CleanupBackup(backupDir string, backupID string) error {
	return backup.CleanupBackup(backupDir, backupID)
}

func ListBackups(backupDir string) ([]string, error) {
	return backup.ListBackups(backupDir)
}

func copyFile(src, dst string, mode os.FileMode) error {
	return backup.NewFileCopier().Copy(src, dst, mode)
}

func backupPathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
