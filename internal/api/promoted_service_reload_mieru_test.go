package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPromotedServiceReloaderRestartsMieruWhenMieruConfigPromoted(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	reloader := service.NewPromotedServiceReloader(root, NewManagedRuntimeCatalog(), func(command []string) ServiceActionResult {
		commands = append(commands, append([]string(nil), command...))
		return ServiceActionResult{Success: true}
	})
	results := reloader.Reload([]string{filepath.Join(root, "live", "mieru", "server_config.json")})
	if len(results) != 1 || len(commands) != 1 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	want := []string{"systemctl", "restart", "veil-mieru.service"}
	for i := range want {
		if commands[0][i] != want[i] {
			t.Fatalf("commands = %+v, want %+v", commands, want)
		}
	}
}

func TestServiceCommandPolicyAllowsMieruRestartButNotNaiveRestart(t *testing.T) {
	policy := service.NewCommandPolicy(NewManagedRuntimeCatalog())
	if !policy.AllowsAction([]string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatal("Mieru restart should be allowed")
	}
	if policy.AllowsAction([]string{"systemctl", "restart", "veil-naive.service"}) {
		t.Fatal("Naive restart should not be allowed by promoted service policy")
	}
}
