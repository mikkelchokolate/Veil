package backup

import "os"

type SafetyPolicy struct{}

type BackupSafetyPolicy = SafetyPolicy

func NewSafetyPolicy() SafetyPolicy { return SafetyPolicy{} }

func NewBackupSafetyPolicy() BackupSafetyPolicy { return NewSafetyPolicy() }

func (SafetyPolicy) ExistingOriginalPaths(manifest Manifest) []string {
	var existingPaths []string
	for _, entry := range manifest.Entries {
		if _, err := os.Stat(entry.OriginalPath); err == nil {
			existingPaths = append(existingPaths, entry.OriginalPath)
		}
	}
	return existingPaths
}
