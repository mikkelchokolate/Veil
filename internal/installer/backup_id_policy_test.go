package installer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBackupIDPolicyAddsSuffixUntilFreeID(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 5, 7, 12, 30, 45, 0, time.UTC) }
	existing := map[string]bool{
		filepath.Join("/backups", "20260507_123045"):   true,
		filepath.Join("/backups", "20260507_123045_1"): true,
	}
	id, err := NewBackupIDPolicy(now, func(path string) (bool, error) { return existing[path], nil }).Next("/backups")
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if id != "20260507_123045_2" {
		t.Fatalf("id = %q", id)
	}
}
