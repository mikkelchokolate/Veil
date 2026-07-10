package api

import (
	"path/filepath"
	"testing"
)

func TestManagedRuntimeCatalogCentralizesCanonicalUnits(t *testing.T) {
	catalog := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "caddy"}, []Inbound{
		{Name: "h", Protocol: "hysteria2", Enabled: true},
		{Name: "m", Protocol: "mieru", Enabled: true},
		{Name: "o", Protocol: "olcrtc", Enabled: true},
		{Name: "n", Protocol: "naiveproxy", Enabled: true},
	}, WarpConfig{Enabled: true})

	want := map[string]struct {
		name, actionName string
	}{
		"veil.service":             {"veil", "veil"},
		"veil-caddy@panel.service": {"caddy-panel", "caddy-panel"},
		"veil-hysteria2@h.service": {"hysteria2-h", "hysteria2-h"},
		"veil-mieru.service":       {"mieru", "mieru"},
		"veil-olcrtc@o.service":    {"olcrtc-o", "olcrtc-o"},
		"veil-caddy@n.service":     {"caddy-n", "caddy-n"},
		"veil-warp.service":        {"sing-box", "sing-box"},
	}

	runtimes := catalog.Runtimes()
	if len(runtimes) != len(want) {
		t.Fatalf("runtimes = %+v", runtimes)
	}
	for _, got := range runtimes {
		expected, ok := want[got.Unit]
		if !ok {
			t.Fatalf("unexpected runtime = %+v", got)
		}
		if got.Name != expected.name || got.ActionName != expected.actionName {
			t.Fatalf("runtime for unit %q = %+v, want %+v", got.Unit, got, expected)
		}
	}
}

func TestManagedRuntimeCatalogBuildsApplyActionsForProtocolsAndWarp(t *testing.T) {
	catalog := NewManagedRuntimeCatalogFor(Settings{}, []Inbound{
		{Name: "n", Protocol: "naiveproxy", Enabled: true},
		{Name: "h", Protocol: "hysteria2", Enabled: true},
		{Name: "m", Protocol: "mieru", Enabled: true},
		{Name: "o", Protocol: "olcrtc", Enabled: true},
	}, WarpConfig{Enabled: true})
	for _, tc := range []struct{ key, action string }{
		{"naiveproxy", "reload veil-caddy@n.service"},
		{"hysteria2", "restart veil-hysteria2@h.service"},
		{"mieru", "restart veil-mieru.service"},
		{"sing-box", "restart veil-warp.service"},
		{"olcrtc", "restart veil-olcrtc@o.service"},
	} {
		action, ok := catalog.ApplyAction(tc.key)
		if !ok || action != tc.action {
			t.Fatalf("ApplyAction(%q) = %q %v, want %q", tc.key, action, ok, tc.action)
		}
	}
}

func TestManagedRuntimeCatalogBuildsServiceActionCommandsFromCanonicalUnits(t *testing.T) {
	command, ok := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "caddy"}, nil, WarpConfig{}).ServiceActionCommand("caddy-panel", "restart")
	if !ok {
		t.Fatal("caddy-panel action name should map to managed panel Caddy runtime")
	}
	want := []string{"systemctl", "restart", "veil-caddy@panel.service"}
	if !equalStrings(command, want) {
		t.Fatalf("command = %+v, want %+v", command, want)
	}
}

func TestManagedRuntimeCatalogBuildsPromotedCommandsFromLiveFiles(t *testing.T) {
	root := t.TempDir()
	catalog := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "caddy"}, []Inbound{{Name: "m", Protocol: "mieru", Enabled: true}}, WarpConfig{})
	commands := catalog.PromotedCommands(root, []string{
		filepath.Join(root, "live", "caddy", "panel.Caddyfile"),
		filepath.Join(root, "live", "mieru", "server_config.json"),
	})
	want := [][]string{
		{"systemctl", "reload", "veil-caddy@panel.service"},
		{"systemctl", "restart", "veil-mieru.service"},
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
	catalog := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "caddy"}, []Inbound{
		{Name: "h", Protocol: "hysteria2", Enabled: true},
		{Name: "m", Protocol: "mieru", Enabled: true},
		{Name: "o", Protocol: "olcrtc", Enabled: true},
	}, WarpConfig{Enabled: true})
	for _, command := range [][]string{
		{"systemctl", "reload", "veil-caddy@panel.service"},
		{"systemctl", "restart", "veil-hysteria2@h.service"},
		{"systemctl", "restart", "veil-warp.service"},
		{"systemctl", "restart", "veil-mieru.service"},
		{"systemctl", "restart", "veil-olcrtc@o.service"},
	} {
		if !catalog.AllowsPromotedAction(command) {
			t.Fatalf("expected promoted command allowed: %+v", command)
		}
	}
	if catalog.AllowsPromotedAction([]string{"systemctl", "restart", "veil-caddy@panel.service"}) {
		t.Fatal("Caddy panel has reload semantics, restart should not be promoted")
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

	catalog := NewManagedRuntimeCatalogFor(Settings{}, inbounds, warp)
	runtimes := catalog.Runtimes()

	// Veil is always present
	// hysteria2 has 2 named units
	// olcrtc has 1 named unit
	// naive proxy has one unit per enabled inbound
	// sing-box is present because warp is enabled
	expectedUnits := map[string]bool{
		"veil.service":                  true,
		"veil-hysteria2@vip.service":    true,
		"veil-hysteria2@public.service": true,
		"veil-olcrtc@rtc1.service":      true,
		"veil-caddy@caddy-a.service":    true,
		"veil-caddy@caddy-b.service":    true,
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
	if !catalog.AllowsPromotedAction([]string{"systemctl", "restart", "veil-hysteria2@vip.service"}) {
		t.Error("expected restart allowed for veil-hysteria2@vip.service")
	}
	if !catalog.AllowsPromotedAction([]string{"systemctl", "restart", "veil-hysteria2@public.service"}) {
		t.Error("expected restart allowed for veil-hysteria2@public.service")
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
