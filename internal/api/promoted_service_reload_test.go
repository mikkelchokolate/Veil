package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPromotedServiceReloaderRunsExpectedReloads(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	// Per-inbound runtimes: hysteria2/edge.yaml promotes to a restart of the
	// veil-hysteria2@edge.service instance — never the bare template unit (#780).
	// panelAccess=caddy keeps the consolidated veil-caddy.service runtime in
	// the catalog so the caddy/config.json promotion also resolves.
	catalog := NewManagedRuntimeCatalogFor(Settings{PanelAccess: "caddy"}, []Inbound{
		{Name: "edge", Protocol: "hysteria2", Enabled: true},
	}, WarpConfig{})
	reloader := service.NewPromotedServiceReloader(root, catalog, func(command []string) ServiceActionResult {
		commands = append(commands, append([]string(nil), command...))
		return ServiceActionResult{Success: true}
	})

	results := reloader.Reload([]string{
		filepath.Join(root, "live", "caddy", "config.json"),
		filepath.Join(root, "live", "hysteria2", "edge.yaml"),
	})
	if len(results) != 2 || len(commands) != 2 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	// Catalog order is name-sorted: caddy reload precedes the hy2 restart.
	if commands[0][2] != "veil-caddy.service" || commands[1][2] != "veil-hysteria2@edge.service" {
		t.Fatalf("commands = %+v", commands)
	}
	if commands[0][1] != "reload" || commands[1][1] != "restart" {
		t.Fatalf("commands = %+v", commands)
	}
	if results[0].Name != "veil-caddy.service" || results[1].Name != "veil-hysteria2@edge.service" {
		t.Fatalf("result names = %+v", results)
	}
}
