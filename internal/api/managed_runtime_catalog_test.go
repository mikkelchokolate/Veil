package api

import (
	"path/filepath"
	"testing"
)

func TestManagedRuntimeCatalogCentralizesCanonicalUnits(t *testing.T) {
	catalog := NewManagedRuntimeCatalog()
	want := []struct {
		name, actionName, unit string
	}{
		{"veil", "veil", "veil.service"},
		{"hysteria2", "hysteria2", "veil-hysteria2@.service"},
		{"mieru", "mieru", "veil-mieru.service"},
		{"naive", "caddy", "veil-naive.service"},
		{"olcrtc", "olcrtc", "veil-olcrtc@.service"},
		{"sing-box", "sing-box", "veil-warp.service"},
	}
	runtimes := catalog.Runtimes()
	if len(runtimes) != len(want) {
		t.Fatalf("runtimes = %+v", runtimes)
	}
	for i, expected := range want {
		got := runtimes[i]
		if got.Name != expected.name || got.ActionName != expected.actionName || got.Unit != expected.unit {
			t.Fatalf("runtime[%d] = %+v, want %+v", i, got, expected)
		}
	}
}

func TestManagedRuntimeCatalogBuildsApplyActionsForProtocolsAndWarp(t *testing.T) {
	catalog := NewManagedRuntimeCatalog()
	for _, tc := range []struct{ key, action string }{
		{"naiveproxy", "reload veil-naive.service"},
		{"hysteria2", "reload veil-hysteria2@.service"},
		{"mieru", "restart veil-mieru.service"},
		{"sing-box", "reload veil-warp.service"},
		{"olcrtc", "restart veil-olcrtc@.service"},
	} {
		action, ok := catalog.ApplyAction(tc.key)
		if !ok || action != tc.action {
			t.Fatalf("ApplyAction(%q) = %q %v, want %q", tc.key, action, ok, tc.action)
		}
	}
}

func TestManagedRuntimeCatalogBuildsServiceActionCommandsFromCanonicalUnits(t *testing.T) {
	command, ok := NewManagedRuntimeCatalog().ServiceActionCommand("caddy", "restart")
	if !ok {
		t.Fatal("caddy action name should map to managed Naive runtime")
	}
	want := []string{"systemctl", "restart", "veil-naive.service"}
	if !equalStrings(command, want) {
		t.Fatalf("command = %+v, want %+v", command, want)
	}
}

func TestManagedRuntimeCatalogBuildsPromotedCommandsFromLiveFiles(t *testing.T) {
	root := t.TempDir()
	commands := NewManagedRuntimeCatalog().PromotedCommands(root, []string{
		filepath.Join(root, "live", "caddy", "Caddyfile"),
		filepath.Join(root, "live", "mieru", "server_config.json"),
	})
	want := [][]string{
		{"systemctl", "restart", "veil-mieru.service"},
		{"systemctl", "reload", "veil-naive.service"},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %+v", commands)
	}
	for i := range want {
		if !equalStrings(commands[i], want[i]) {
			t.Fatalf("commands = %+v, want %+v", commands, want)
		}
	}
}

func TestManagedRuntimeCatalogAllowsOnlyPromotedApplyCommands(t *testing.T) {
	catalog := NewManagedRuntimeCatalog()
	for _, command := range [][]string{
		{"systemctl", "reload", "veil-naive.service"},
		{"systemctl", "reload", "veil-hysteria2@.service"},
		{"systemctl", "reload", "veil-warp.service"},
		{"systemctl", "restart", "veil-mieru.service"},
		{"systemctl", "restart", "veil-olcrtc@.service"},
	} {
		if !catalog.AllowsPromotedAction(command) {
			t.Fatalf("expected promoted command allowed: %+v", command)
		}
	}
	if catalog.AllowsPromotedAction([]string{"systemctl", "reload", "veil-mieru.service"}) {
		t.Fatal("Mieru has no reload semantics in its managed unit")
	}
}

func TestNewManagedRuntimeCatalogForMultipleInbounds(t *testing.T) {
	inbounds := []Inbound{
		{Name: "vip", Protocol: "hysteria2", Enabled: true},
		{Name: "public", Protocol: "hysteria2", Enabled: true},
		{Name: "rtc1", Protocol: "olcrtc", Enabled: true},
		{Name: "caddy-a", Protocol: "naiveproxy", Enabled: true},
		{Name: "caddy-b", Protocol: "naiveproxy", Enabled: true},
	}
	warp := WarpConfig{Enabled: true}

	catalog := NewManagedRuntimeCatalogFor(inbounds, warp)
	runtimes := catalog.Runtimes()

	// Veil is always present
	// hysteria2 has 2 named units
	// olcrtc has 1 named unit
	// naive proxy has 1 aggregated caddy unit
	// sing-box is present because warp is enabled
	expectedUnits := map[string]bool{
		"veil.service":                  true,
		"veil-hysteria2@vip.service":    true,
		"veil-hysteria2@public.service": true,
		"veil-olcrtc@rtc1.service":      true,
		"veil-naive.service":            true,
		"veil-warp.service":             true,
	}

	foundUnits := make(map[string]bool)
	for _, rt := range runtimes {
		foundUnits[rt.Unit] = true
	}

	if len(foundUnits) != len(expectedUnits) {
		t.Fatalf("expected %d unique units, got %d. runtimes = %+v", len(expectedUnits), len(foundUnits), runtimes)
	}

	for unit := range expectedUnits {
		if !foundUnits[unit] {
			t.Errorf("expected unit %q not found in catalog", unit)
		}
	}

	// Verify status action rules & health check checks
	if !catalog.AllowsPromotedAction([]string{"systemctl", "reload", "veil-hysteria2@vip.service"}) {
		t.Error("expected reload allowed for veil-hysteria2@vip.service")
	}
	if !catalog.AllowsPromotedAction([]string{"systemctl", "reload", "veil-hysteria2@public.service"}) {
		t.Error("expected reload allowed for veil-hysteria2@public.service")
	}
	if catalog.AllowsPromotedAction([]string{"systemctl", "reload", "veil-olcrtc@rtc1.service"}) {
		t.Error("expected reload NOT allowed for veil-olcrtc@rtc1.service (restart only)")
	}
	if !catalog.AllowsPromotedAction([]string{"systemctl", "restart", "veil-olcrtc@rtc1.service"}) {
		t.Error("expected restart allowed for veil-olcrtc@rtc1.service")
	}

	// Verify health check allowance
	if !catalog.AllowsHealthUnit("veil-hysteria2@vip.service") {
		t.Error("expected health checks allowed for veil-hysteria2@vip.service")
	}
	if !catalog.AllowsHealthUnit("veil-olcrtc@rtc1.service") {
		t.Error("expected health checks allowed for veil-olcrtc@rtc1.service")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
