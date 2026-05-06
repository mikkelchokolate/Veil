package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveConfigPromotionPromotesBacksUpAndRollsBack(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "Caddyfile")
	live := filepath.Join(root, "live", "caddy", "Caddyfile")
	if err := writeAtomicFile(staged, []byte("new"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if err := writeAtomicFile(live, []byte("old"), 0o600); err != nil {
		t.Fatalf("write live: %v", err)
	}
	promotion := NewLiveConfigPromotion(root, nil)

	liveFiles, backupFiles, records, err := promotion.Promote([]string{staged})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(liveFiles) != 1 || liveFiles[0] != live || len(backupFiles) != 1 || len(records) != 1 || !records[0].HadPrevious {
		t.Fatalf("promotion result: live=%+v backups=%+v records=%+v", liveFiles, backupFiles, records)
	}
	assertFileBody(t, live, "new")

	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 1 || rollbackFiles[0] != live {
		t.Fatalf("rollback files = %+v", rollbackFiles)
	}
	assertFileBody(t, live, "old")
}

func assertFileBody(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(body) != want {
		t.Fatalf("%s = %q, want %q", path, body, want)
	}
}
