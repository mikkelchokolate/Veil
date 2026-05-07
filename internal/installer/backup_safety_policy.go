package installer

import "os"

type BackupSafetyPolicy struct{}

func NewBackupSafetyPolicy() BackupSafetyPolicy { return BackupSafetyPolicy{} }

func (BackupSafetyPolicy) ExistingOriginalPaths(manifest backupManifest) []string {
	var existingPaths []string
	for _, entry := range manifest.Entries {
		if _, err := os.Stat(entry.OriginalPath); err == nil {
			existingPaths = append(existingPaths, entry.OriginalPath)
		}
	}
	return existingPaths
}
