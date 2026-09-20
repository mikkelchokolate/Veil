package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/protocols"
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

	orphanCaddy := filepath.Join(root, "live", "caddy", "orphan.json")
	legacyCaddy := filepath.Join(root, "live", "caddy", "legacy.Caddyfile")
	orphanHysteria2 := filepath.Join(root, "live", "hysteria2", "orphan.yaml")
	staleCaddySingleton := filepath.Join(root, "live", "caddy", "config.json")
	aggregateOnlyMieruSidecar := filepath.Join(root, "live", "mieru", "sidecar.json")

	if err := atomicfile.Write(orphanCaddy, []byte("orphan caddy content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan caddy: %v", err)
	}
	if err := atomicfile.Write(legacyCaddy, []byte("legacy caddy content"), 0o600, 0o700); err != nil {
		t.Fatalf("write legacy caddy: %v", err)
	}
	if err := atomicfile.Write(orphanHysteria2, []byte("orphan hysteria2 content"), 0o600, 0o700); err != nil {
		t.Fatalf("write orphan hysteria2: %v", err)
	}
	if err := atomicfile.Write(staleCaddySingleton, []byte("stale consolidated caddy"), 0o600, 0o700); err != nil {
		t.Fatalf("write stale caddy singleton: %v", err)
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

	// Five backups: the retired per-inbound Caddy JSON, the legacy Caddyfile,
	// the stale consolidated config.json (no runtime needs it in this apply —
	// the aggregate caddy dir scan deliberately carries no excluded base
	// name), the orphan hysteria2, and the aggregate Mieru sidecar.
	if len(backupFiles) != 5 {
		t.Fatalf("expected 5 backup files, got %+v", backupFiles)
	}

	if _, err := os.Stat(orphanCaddy); !os.IsNotExist(err) {
		t.Fatalf("orphan caddy file should be removed, but stat got: %v", err)
	}
	if _, err := os.Stat(legacyCaddy); !os.IsNotExist(err) {
		t.Fatalf("legacy caddy file should be removed, but stat got: %v", err)
	}
	if _, err := os.Stat(orphanHysteria2); !os.IsNotExist(err) {
		t.Fatalf("orphan hysteria2 file should be removed, but stat got: %v", err)
	}
	// With aggregate protocol directories also scanned, the mieru sidecar
	// file is not a promoted artifact and is treated as an orphan.
	if _, err := os.Stat(aggregateOnlyMieruSidecar); !os.IsNotExist(err) {
		t.Fatalf("aggregate-only mieru sidecar should be removed as orphan, but stat got: %v", err)
	}
	// The consolidated singleton is orphaned too when no runtime needs it:
	// leaving it would keep stale auth_credentials on disk (audit #123).
	if _, err := os.Stat(staleCaddySingleton); !os.IsNotExist(err) {
		t.Fatalf("stale caddy singleton should be removed as orphan, but stat got: %v", err)
	}

	rollbackFiles, _ := promotion.Rollback(records, liveFiles)
	if len(rollbackFiles) != 6 {
		t.Fatalf("expected 6 rollback files, got %+v", rollbackFiles)
	}

	assertFileBody(t, orphanCaddy, "orphan caddy content")
	assertFileBody(t, legacyCaddy, "legacy caddy content")
	assertFileBody(t, orphanHysteria2, "orphan hysteria2 content")
	assertFileBody(t, staleCaddySingleton, "stale consolidated caddy")
	assertFileBody(t, aggregateOnlyMieruSidecar, "do not scan aggregate-only dir")
	if _, err := os.Stat(liveMieru); !os.IsNotExist(err) {
		t.Fatalf("new live file should be removed on rollback, stat got: %v", err)
	}
}

func TestLiveConfigOrphanDirsComeFromTemplateAndAggregateProtocolPlugins(t *testing.T) {
	got := liveConfigOrphanDirs()
	want := []liveConfigOrphanDir{
		{subpath: "caddy", ext: ".Caddyfile"},
		// NaiveProxy/Caddy is a single consolidated veil-caddy.service backed
		// by caddy/config.json — an aggregate artifact, not a per-inbound
		// template, so the JSON dir scans without an excluded base name
		// (config.json itself is cleaned once no runtime needs it).
		{subpath: "caddy", ext: ".json"},
		{subpath: "hysteria2", ext: ".yaml", exclude: "server.yaml"},
		{subpath: "mieru", ext: ".json"},
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

// The consolidated veil-caddy.service is not a systemd template: naiveproxy
// must not be classified as a per-inbound template protocol, and stray
// caddy/<name>.json files must not be promotable as template instances.
func TestNaiveProxyIsNotATemplateRuntime(t *testing.T) {
	registry := protocols.NewRegistry()
	naive, ok := registry.Get("naiveproxy")
	if !ok {
		t.Fatal("naiveproxy plugin not registered")
	}
	if hasTemplateRuntime(naive) {
		t.Fatal("naiveproxy must not be treated as a template runtime")
	}
	cr, ok := protocols.AsConfigRenderer(naive)
	if !ok {
		t.Fatal("naiveproxy must render config")
	}
	sub := cr.ArtifactSpec().Subpath
	if isPromotableDynamicProtocolArtifact(naive, sub, "caddy/foo.json") {
		t.Fatal("stray caddy/<name>.json must not be a promotable per-inbound artifact")
	}
	// The real consolidated artifact stays resolvable to veil-caddy.service.
	if unit, ok := UnitForArtifactID("caddy/config.json"); !ok || unit != unitCaddy {
		t.Fatalf("caddy/config.json unit = %q ok=%v, want %q", unit, ok, unitCaddy)
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
			staged: filepath.Join(root, "generated", "caddy", "config.json"),
			live:   filepath.Join(root, "live", "caddy", "config.json"),
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
			staged: filepath.Join(root, "generated", "rules", "geoip.dat"),
			live:   filepath.Join(root, "live", "rules", "geoip.dat"),
			ok:     true,
		},
		{
			staged: filepath.Join(root, "generated", "rules", "geosite.dat"),
			live:   filepath.Join(root, "live", "rules", "geosite.dat"),
			ok:     true,
		},
		{
			staged: filepath.Join(root, "generated", "rules", "other.dat"),
			ok:     false,
		},
		{
			staged: filepath.Join(root, "generated", "hysteria2", "edge.txt"),
			ok:     false,
		},
		{
			staged: filepath.Join(root, "generated", "hysteria2", "bad.name.yaml"),
			ok:     false,
		},
		{
			staged: filepath.Join(root, "generated", "hysteria2", "nested", "edge.yaml"),
			ok:     false,
		},
		{
			staged: filepath.Join(root, "generated", "mieru", "sidecar.json"),
			ok:     false,
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

func TestScanEnabledOrphanTemplateUnitsSkipsDesiredAndUnsafe(t *testing.T) {
	wants := t.TempDir()
	for _, name := range []string{
		"veil-hysteria2@keep.service",
		"veil-hysteria2@hy2-auto.service",
		"veil-olcrtc@old.service",
		"veil-hysteria2@bad.name.service",
		"veil.service",
		"unrelated.service",
	} {
		path := filepath.Join(wants, name)
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	got := scanEnabledOrphanTemplateUnits(wants, map[string]struct{}{
		"veil-hysteria2@keep.service": {},
	})
	want := []string{"veil-hysteria2@hy2-auto.service", "veil-olcrtc@old.service"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("orphans = %v, want %v", got, want)
	}
}

func TestScanEnabledOrphanTemplateUnitsEmptyDirIsNoop(t *testing.T) {
	if got := scanEnabledOrphanTemplateUnits("", map[string]struct{}{"veil-hysteria2@x.service": {}}); got != nil {
		t.Fatalf("empty wants dir = %v", got)
	}
	if got := scanEnabledOrphanTemplateUnits(filepath.Join(t.TempDir(), "missing"), nil); got != nil {
		t.Fatalf("missing wants dir = %v", got)
	}
}

func TestUnitForPathRejectsUnsafeDynamicArtifactNames(t *testing.T) {
	if unit, ok := UnitForArtifactID("hysteria2/bad.name.yaml"); ok || unit != "" {
		t.Fatalf("UnitForArtifactID unsafe name = %q %v", unit, ok)
	}
	if unit, ok := UnitForArtifactID("hysteria2/edge.yaml"); !ok || unit != "veil-hysteria2@edge.service" {
		t.Fatalf("UnitForArtifactID safe name = %q %v", unit, ok)
	}
	if unit, ok := UnitForArtifactID("caddy/legacy.Caddyfile"); !ok || unit != "veil-caddy@legacy.service" {
		t.Fatalf("UnitForArtifactID legacy Caddyfile = %q %v", unit, ok)
	}
	if unit, ok := UnitForArtifactID("caddy/bad.name.Caddyfile"); ok || unit != "" {
		t.Fatalf("UnitForArtifactID unsafe legacy Caddyfile = %q %v", unit, ok)
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
