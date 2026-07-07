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
	staged := filepath.Join(root, "generated", "caddy", "Caddyfile")
	live := filepath.Join(root, "live", "caddy", "Caddyfile")
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

	orphanCaddy := filepath.Join(root, "live", "caddy", "orphan.Caddyfile")
	orphanHysteria2 := filepath.Join(root, "live", "hysteria2", "orphan.yaml")
	nonOrphanCaddy := filepath.Join(root, "live", "caddy", "panel.Caddyfile")
	aggregateOnlyMieruSidecar := filepath.Join(root, "live", "mieru", "sidecar.json")

	if err := atomicfile.Write(orphanCaddy, []byte("orphan caddy content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan caddy: %v", err)
	}
	if err := atomicfile.Write(orphanHysteria2, []byte("orphan hysteria2 content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
	}
	if err := atomicfile.Write(nonOrphanCaddy, []byte("non-orphan caddy"), 0o600, 0o700); err != nil {
		t.Fatalf("write non-orphan caddy: %v", err)
	}
	if err := atomicfile.Write(aggregateOnlyMieruSidecar, []byte("do not scan aggregate-only dir"), 0o600, 0o700); err != nil {
		t.Fatalf("write mieru sidecar: %v", err)
	}

	promotion := NewLiveConfigPromotion(root, nil)

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

	if _, err := os.Stat(orphanCaddy); !os.IsNotExist(err) {
		t.Fatalf("orphan caddy file should be removed, but stat got: %v", err)
	}
	if _, err := os.Stat(orphanHysteria2); !os.IsNotExist(err) {
		t.Fatalf("orphan hysteria2 file should be removed, but stat got: %v", err)
	}
	assertFileBody(t, nonOrphanCaddy, "non-orphan caddy")
	assertFileBody(t, aggregateOnlyMieruSidecar, "do not scan aggregate-only dir")

	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 3 {
		t.Fatalf("expected 3 rollback files, got %+v", rollbackFiles)
	}

	assertFileBody(t, orphanCaddy, "orphan caddy content")
	assertFileBody(t, orphanHysteria2, "orphan hysteria2 content")
	if _, err := os.Stat(liveMieru); !os.IsNotExist(err) {
		t.Fatalf("new live file should be removed on rollback, stat got: %v", err)
	}
}

func TestLiveConfigOrphanDirsComeFromTemplateProtocolPlugins(t *testing.T) {
	got := liveConfigOrphanDirs()
	want := []liveConfigOrphanDir{
		{subpath: "caddy", ext: ".Caddyfile", exclude: "panel.Caddyfile"},
		{subpath: "hysteria2", ext: ".yaml", exclude: "server.yaml"},
		{subpath: "olcrtc", ext: ".yaml", exclude: "server.yaml"},
	}
	if len(got) != len(want) {
		t.Fatalf("liveConfigOrphanDirs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("liveConfigOrphanDirs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLivePathForStagedConfigUsesPluginAndWarpArtifacts(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		staged string
		live   string
		ok     bool
	}{
		{
			staged: filepath.Join(root, "generated", "hysteria2", "edge.yaml"),
			live:   filepath.Join(root, "live", "hysteria2", "edge.yaml"),
			ok:     true,
		},
		{
			staged: filepath.Join(root, "generated", "mieru", "server_config.json"),
			live:   filepath.Join(root, "live", "mieru", "server_config.json"),
			ok:     true,
		},
		{
			staged: filepath.Join(root, "generated", "sing-box", "warp.json"),
			live:   filepath.Join(root, "live", "sing-box", "warp.json"),
			ok:     true,
		},
		{
			staged: filepath.Join(root, "generated", "unknown", "config.json"),
			ok:     false,
		},
	}
	for _, tc := range cases {
		got, ok := livePathForStagedConfig(root, tc.staged)
		if ok != tc.ok || got != tc.live {
			t.Fatalf("livePathForStagedConfig(%q) = (%q, %v), want (%q, %v)", tc.staged, got, ok, tc.live, tc.ok)
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
