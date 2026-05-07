package api

import (
	"path/filepath"
	"testing"
)

func TestPromotedServiceActionCatalogMapsLiveFilesToCommands(t *testing.T) {
	root := t.TempDir()
	commands := NewPromotedServiceActionCatalog(root).Commands([]string{
		filepath.Join(root, "live", "caddy", "Caddyfile"),
		filepath.Join(root, "live", "hysteria2", "server.yaml"),
		filepath.Join(root, "live", "sing-box", "warp.json"),
		filepath.Join(root, "live", "mieru", "server_config.json"),
	})
	want := [][]string{
		{"systemctl", "reload", "veil-naive.service"},
		{"systemctl", "reload", "veil-hysteria2.service"},
		{"systemctl", "reload", "veil-warp.service"},
		{"systemctl", "restart", "veil-mieru.service"},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %+v", commands)
	}
	for i := range want {
		for j := range want[i] {
			if commands[i][j] != want[i][j] {
				t.Fatalf("commands = %+v, want %+v", commands, want)
			}
		}
	}
}
