package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPromotedServiceReloaderRunsExpectedReloads(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	reloader := service.NewPromotedServiceReloader(root, NewManagedRuntimeCatalog(), func(command []string) ServiceActionResult {
		commands = append(commands, append([]string(nil), command...))
		return ServiceActionResult{Success: true}
	})

	results := reloader.Reload([]string{
		filepath.Join(root, "live", "caddy", "panel.Caddyfile"),
		filepath.Join(root, "live", "hysteria2", "server.yaml"),
	})
	if len(results) != 2 || len(commands) != 2 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	if commands[0][2] != "veil-caddy@panel.service" || commands[1][2] != "veil-hysteria2@.service" {
		t.Fatalf("commands = %+v", commands)
	}
	if results[0].Name != "veil-caddy@panel.service" || results[1].Name != "veil-hysteria2@.service" {
		t.Fatalf("result names = %+v", results)
	}
}
