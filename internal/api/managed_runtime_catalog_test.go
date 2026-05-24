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
		{"naive", "caddy", "veil-naive.service"},
		{"hysteria2", "hysteria2", "veil-hysteria2.service"},
		{"sing-box", "sing-box", "veil-warp.service"},
		{"mieru", "mieru", "veil-mieru.service"},
		{"olcrtc", "olcrtc", "veil-olcrtc.service"},
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
		{"hysteria2", "reload veil-hysteria2.service"},
		{"mieru", "restart veil-mieru.service"},
		{"sing-box", "reload veil-warp.service"},
		{"olcrtc", "reload veil-olcrtc.service"},
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
		{"systemctl", "reload", "veil-naive.service"},
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
	catalog := NewManagedRuntimeCatalog()
	for _, command := range [][]string{
		{"systemctl", "reload", "veil-naive.service"},
		{"systemctl", "reload", "veil-hysteria2.service"},
		{"systemctl", "reload", "veil-warp.service"},
		{"systemctl", "restart", "veil-mieru.service"},
		{"systemctl", "reload", "veil-olcrtc.service"},
	} {
		if !catalog.AllowsPromotedAction(command) {
			t.Fatalf("expected promoted command allowed: %+v", command)
		}
	}
	if catalog.AllowsPromotedAction([]string{"systemctl", "reload", "veil-mieru.service"}) {
		t.Fatal("Mieru has no reload semantics in its managed unit")
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
