package backup

import (
	"errors"
	"testing"
	"time"
)

func TestBackupIDPolicyExistsError(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 5, 7, 12, 30, 45, 0, time.UTC) }
	_, err := NewBackupIDPolicy(now, func(string) (bool, error) {
		return false, errors.New("injected exists failure")
	}).Next("/backups")
	if err == nil {
		t.Fatal("expected error from exists failure")
	}
}
