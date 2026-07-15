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

	// Create orphaned configs. Caddy artifacts are now exclusively
	// live/caddy/config.json and are no longer scanned as orphans.
	orphanHysteria2 := filepath.Join(root, "live", "hysteria2", "orphan.yaml")

	if err := atomicfile.Write(orphanHysteria2, []byte("orphan hysteria2 content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
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

	if len(backupFiles) != 1 {
		t.Fatalf("expected 1 backup file, got %+v", backupFiles)
	}

	// Verify that the hysteria2 orphan was removed from live paths
	if _, err := os.Stat(orphanHysteria2); !os.IsNotExist(err) {
		t.Fatalf("orphan hysteria2 file should be removed, but stat got: %v", err)
	}

	// Rollback
	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 2 {
		t.Fatalf("expected 2 rollback files, got %+v", rollbackFiles)
	}

	// Check that the hysteria2 orphan is restored
	assertFileBody(t, orphanHysteria2, "orphan hysteria2 content")
	// Check that new live file was removed
	if _, err := os.Stat(liveMieru); !os.IsNotExist(err) {
		t.Fatalf("new live file should be removed on rollback, stat got: %v", err)
	}
}

// TestLiveConfigPromotionLegacyCaddyArtifactCleaned verifies that legacy
// per-inbound Caddy Caddyfiles (live/caddy/<name>.Caddyfile) from the
// pre-redesign model are detected as orphans, backed up, removed, and mapped
// back to the old per-inbound systemd unit for stop/disable.
func TestLiveConfigPromotionLegacyCaddyArtifactCleaned(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "config.json")
	legacy := filepath.Join(root, "live", "caddy", "legacy.Caddyfile")
	if err := atomicfile.Write(staged, []byte("new json"), 0o600, 0o700); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if err := atomicfile.Write(legacy, []byte("legacy caddyfile"), 0o600, 0o700); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	promotion := NewLiveConfigPromotion(root, nil)
	liveFiles, backupFiles, records, err := promotion.Promote([]string{staged})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(liveFiles) != 1 {
		t.Fatalf("expected 1 promoted file, got %+v", liveFiles)
	}
	if len(backupFiles) != 1 {
		t.Fatalf("expected 1 backup file (legacy Caddyfile), got %+v", backupFiles)
	}

	// Legacy per-inbound Caddyfile is removed.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy caddyfile should be removed, stat err: %v", err)
	}

	// The artifact maps back to the old per-inbound template unit.
	unit, ok := UnitForArtifactID("caddy/legacy.Caddyfile")
	if !ok {
		t.Fatal("expected unit for legacy caddy artifact")
	}
	if unit != "veil-caddy@legacy.service" {
		t.Fatalf("unit = %q, want veil-caddy@legacy.service", unit)
	}

	// Rollback restores the legacy artifact.
	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 2 {
		t.Fatalf("expected 2 rollback files, got %+v", rollbackFiles)
	}
	assertFileBody(t, legacy, "legacy caddyfile")
}

func TestUnitForArtifactIDLegacyCaddyCaddyfile(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"caddy/legacy.Caddyfile", "veil-caddy@legacy.service"},
		{"caddy/panel.Caddyfile", "veil-caddy@panel.service"},
		{"generated/caddy/client.Caddyfile", "veil-caddy@client.service"},
	}
	for _, c := range cases {
		unit, ok := UnitForArtifactID(c.id)
		if !ok {
			t.Fatalf("UnitForArtifactID(%q) = false, want true", c.id)
		}
		if unit != c.want {
			t.Fatalf("UnitForArtifactID(%q) = %q, want %q", c.id, unit, c.want)
		}
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
