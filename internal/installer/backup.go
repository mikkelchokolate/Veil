package installer

import (
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
