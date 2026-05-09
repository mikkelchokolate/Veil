package cli

import (
	"path/filepath"
	"testing"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/renderer"
)

func TestPanelStateRepairMaterialAddsGeneratedConfigsAndRuntimeUnits(t *testing.T) {
	dir := t.TempDir()
	opts := repairWorkflowOptions{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	material := newPanelStateRepairMaterial(opts, panelStateRepairSnapshot{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel/", Mode: "server", Domain: "panel.example.com", Email: "admin@example.com"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
		Warp:     api.WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	})

	plan, err := material.Apply(installer.RepairPlan{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, want := range []string{
		filepath.Join(opts.EtcDir, "veil.env"),
		filepath.Join(opts.EtcDir, "generated", "caddy", "Caddyfile"),
		filepath.Join(opts.EtcDir, "generated", "mieru", "server_config.json"),
		filepath.Join(opts.SystemdDir, renderer.UnitNaive),
		filepath.Join(opts.SystemdDir, renderer.UnitMieru),
	} {
		if !repairPlanHasAction(plan, want) {
			t.Fatalf("repair material missing %s in plan: %+v", want, plan.Actions)
		}
	}
}

func repairPlanHasAction(plan installer.RepairPlan, path string) bool {
	for _, action := range plan.Actions {
		if action.Path == path {
			return true
		}
	}
	return false
}
