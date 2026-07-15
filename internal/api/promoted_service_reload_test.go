package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPromotedServiceReloaderRunsExpectedReloads(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	catalog := NewManagedRuntimeCatalogFor(Settings{}, []Inbound{
		{Name: "naive", Protocol: "naiveproxy", Enabled: true},
		{Name: "h", Protocol: "hysteria2", Enabled: true},
	}, WarpConfig{})
	reloader := service.NewPromotedServiceReloader(root, catalog, func(command []string) ServiceActionResult {
		commands = append(commands, append([]string(nil), command...))
		return ServiceActionResult{Success: true}
	})

	results := reloader.Reload([]string{
		filepath.Join(root, "live", "caddy", "config.json"),
		filepath.Join(root, "live", "hysteria2", "h.yaml"),
	})
	if len(results) != 2 || len(commands) != 2 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	if commands[0][2] != "veil-hysteria2@h.service" || commands[1][2] != "veil-caddy.service" {
		t.Fatalf("commands = %+v", commands)
	}
	if results[0].Name != "veil-hysteria2@h.service" || results[1].Name != "veil-caddy.service" {
		t.Fatalf("result names = %+v", results)
	}
}
