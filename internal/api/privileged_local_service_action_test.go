package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestLocalPrivilegedClientReloadFallsBackToStartWhenInactive(t *testing.T) {
	root := t.TempDir()
	state := &managementState{
		statePath:            filepath.Join(root, "state.json"),
		applyRoot:            filepath.Join(root, "apply"),
		liveRoot:             filepath.Join(root, "live"),
		keyPath:              filepath.Join(root, "state.key"),
		backupPassphrasePath: filepath.Join(root, "backup.passphrase"),
		backupDir:            filepath.Join(root, "backups"),
		version:              "v0.0.1",
		settings:             Settings{PanelAccess: "caddy"},
	}

	orig := serviceActionRunner
	defer func() { serviceActionRunner = orig }()

	var calls [][]string
	serviceActionRunner = func(command []string) ServiceActionResult {
		calls = append(calls, append([]string(nil), command...))
		if len(command) >= 3 && command[0] == "systemctl" && command[1] == "is-active" {
			if command[2] == "veil-mieru.service" {
				return ServiceActionResult{Command: command, Success: false, Error: "inactive"}
			}
			return ServiceActionResult{Command: command, Success: true, Output: "active"}
		}
		return ServiceActionResult{Command: command, Success: true}
	}

	client := newLocalPrivilegedClient(state)

	if err := client.ServiceAction(context.Background(), privileged.ServiceActionRequest{
		Unit:   "veil-caddy@panel.service",
		Action: privileged.ServiceActionReload,
	}); err != nil {
		t.Fatalf("reload active unit: %v", err)
	}
	if err := client.ServiceAction(context.Background(), privileged.ServiceActionRequest{
		Unit:   "veil-mieru.service",
		Action: privileged.ServiceActionReload,
	}); err != nil {
		t.Fatalf("reload inactive unit: %v", err)
	}

	want := [][]string{
		{"systemctl", "is-active", "veil-caddy@panel.service"},
		{"systemctl", "reload", "veil-caddy@panel.service"},
		{"systemctl", "is-active", "veil-mieru.service"},
		{"systemctl", "start", "veil-mieru.service"},
	}
	if len(calls) != len(want) {
		t.Fatalf("want %d calls, got %d: %v", len(want), len(calls), calls)
	}
	for i := range want {
		if len(calls[i]) != len(want[i]) {
			t.Fatalf("call %d: want %v, got %v", i, want[i], calls[i])
		}
		for j := range want[i] {
			if calls[i][j] != want[i][j] {
				t.Fatalf("call %d: want %v, got %v", i, want[i], calls[i])
			}
		}
	}
}
