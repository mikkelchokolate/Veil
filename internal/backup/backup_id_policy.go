package backup

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

// backupIDPattern matches IDs produced by IDPolicy.Next: YYYYMMDD_HHMMSS
// with an optional _N suffix when that second already exists.
var backupIDPattern = regexp.MustCompile(`^\d{8}_\d{6}(?:_[1-9]\d*)?$`)

func validateBackupID(id string) error {
	if !backupIDPattern.MatchString(id) || !filepath.IsLocal(id) {
		return fmt.Errorf("invalid backup ID %q", id)
	}
	return nil
}

type IDPolicy struct {
	now    func() time.Time
	exists func(path string) (bool, error)
}

type BackupIDPolicy = IDPolicy

func NewIDPolicy(now func() time.Time, exists func(path string) (bool, error)) IDPolicy {
	return IDPolicy{now: now, exists: exists}
}

func NewBackupIDPolicy(now func() time.Time, exists func(path string) (bool, error)) BackupIDPolicy {
	return NewIDPolicy(now, exists)
}

func (p IDPolicy) Next(dir string) (string, error) {
	baseID := p.now().UTC().Format("20060102_150405")
	backupID := baseID
	for suffix := 1; ; suffix++ {
		backupPath := filepath.Join(dir, backupID)
		exists, err := p.exists(backupPath)
		if err != nil {
			return "", fmt.Errorf("stat backup path %s: %w", backupPath, err)
		}
		if !exists {
			return backupID, nil
		}
		backupID = fmt.Sprintf("%s_%d", baseID, suffix)
	}
}
