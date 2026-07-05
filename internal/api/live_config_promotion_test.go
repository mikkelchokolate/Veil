package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func TestLiveConfigPromotionPromotesMieruConfig(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "mieru", "server_config.json")
	live := filepath.Join(root, "live", "mieru", "server_config.json")
	if err := atomicfile.Write(staged, []byte(`{"portBindings":[],"users":[]}`), 0o600, 0o700); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	promotion := NewLiveConfigPromotion(root, nil)

	liveFiles, _, records, err := promotion.Promote([]string{staged})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(liveFiles) != 1 || liveFiles[0] != live || len(records) != 1 || records[0].LivePath != live {
		t.Fatalf("Mieru promotion result: live=%+v records=%+v", liveFiles, records)
	}
	assertFileBody(t, live, `{"portBindings":[],"users":[]}`)
}

func TestLiveConfigPromotionPromotesBacksUpAndRollsBack(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "config.json")
	live := filepath.Join(root, "live", "caddy", "config.json")
	if err := atomicfile.Write(staged, []byte("new"), 0o600, 0o700); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if err := atomicfile.Write(live, []byte("old"), 0o600, 0o700); err != nil {
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

func TestLiveConfigPromotionOrphans(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "mieru", "server_config.json")
	if err := atomicfile.Write(staged, []byte("new-mieru"), 0o600, 0o700); err != nil {
		t.Fatalf("write staged: %v", err)
	}

	// Create orphaned configs
	orphanCaddy := filepath.Join(root, "live", "caddy", "orphan.Caddyfile")
	orphanHysteria2 := filepath.Join(root, "live", "hysteria2", "orphan.yaml")
	nonOrphanCaddy := filepath.Join(root, "live", "caddy", "panel.Caddyfile") // excluded from scans

	if err := atomicfile.Write(orphanCaddy, []byte("orphan caddy content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan caddy: %v", err)
	}
	if err := atomicfile.Write(orphanHysteria2, []byte("orphan hysteria2 content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
	}
	if err := atomicfile.Write(nonOrphanCaddy, []byte("non-orphan caddy"), 0o600, 0o700); err != nil {
		t.Fatalf("write non-orphan caddy: %v", err)
	}

	promotion := NewLiveConfigPromotion(root, nil)

	// Promote
	liveFiles, backupFiles, records, err := promotion.Promote([]string{staged})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	liveMieru := filepath.Join(root, "live", "mieru", "server_config.json")
	if len(liveFiles) != 1 || liveFiles[0] != liveMieru {
		t.Fatalf("unexpected liveFiles: %+v", liveFiles)
	}

	if len(backupFiles) != 2 {
		t.Fatalf("expected 2 backup files, got %+v", backupFiles)
	}

	// Verify that orphans were removed from live paths
	if _, err := os.Stat(orphanCaddy); !os.IsNotExist(err) {
		t.Fatalf("orphan caddy file should be removed, but stat got: %v", err)
	}
	if _, err := os.Stat(orphanHysteria2); !os.IsNotExist(err) {
		t.Fatalf("orphan hysteria2 file should be removed, but stat got: %v", err)
	}
	// Excluded non-orphan should still exist
	assertFileBody(t, nonOrphanCaddy, "non-orphan caddy")

	// Rollback
	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 3 {
		t.Fatalf("expected 3 rollback files, got %+v", rollbackFiles)
	}

	// Check that orphans are restored
	assertFileBody(t, orphanCaddy, "orphan caddy content")
	assertFileBody(t, orphanHysteria2, "orphan hysteria2 content")
	// Check that new live file was removed
	if _, err := os.Stat(liveMieru); !os.IsNotExist(err) {
		t.Fatalf("new live file should be removed on rollback, stat got: %v", err)
	}
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
