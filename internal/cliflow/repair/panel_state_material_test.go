package repair

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestPanelStateRepairMaterialAddsGeneratedConfigsAndRuntimeUnits(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	customBinary := filepath.Join(dir, "opt", "veil", "bin", "veil")
	material := newPanelStateRepairMaterial(opts, panelStateRepairSnapshot{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel/", Mode: "server", Domain: "panel.example.com", Email: "admin@example.com"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
		Warp:     api.WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}, PlanDependencies{
		Secret:     func(label string) string { return "repair-" + label },
		Executable: func() (string, error) { return customBinary, nil },
	})

	plan, err := material.Apply(installer.RepairPlan{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, want := range []string{
		filepath.Join(opts.EtcDir, "veil.env"),
		filepath.Join(opts.EtcDir, "generated", "caddy", "config.json"),
		filepath.Join(opts.EtcDir, "generated", "mieru", "server_config.json"),
		filepath.Join(opts.SystemdDir, renderer.UnitCaddy),
		filepath.Join(opts.SystemdDir, renderer.UnitMieru),
	} {
		if !repairPlanHasAction(plan, want) {
			t.Fatalf("repair material missing %s in plan: %+v", want, plan.Actions)
		}
	}
	// #1437: the re-rendered units must carry the installed layout and binary.
	mieru := repairPlanActionContent(t, plan, filepath.Join(opts.SystemdDir, renderer.UnitMieru))
	for _, want := range []string{opts.VarDir, customBinary} {
		if !strings.Contains(mieru, want) {
			t.Fatalf("re-rendered mieru unit lost %q:\n%s", want, mieru)
		}
	}
	if strings.Contains(mieru, " /var/lib/veil\n") {
		t.Fatalf("re-rendered mieru unit fell back to the packaged var dir:\n%s", mieru)
	}
}

func repairPlanActionContent(t *testing.T, plan installer.RepairPlan, path string) string {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Path == path {
			return action.Content
		}
	}
	t.Fatalf("repair plan has no action for %s", path)
	return ""
}

func repairPlanHasAction(plan installer.RepairPlan, path string) bool {
	for _, action := range plan.Actions {
		if action.Path == path {
			return true
		}
	}
	return false
}

// TestPanelStateRepairMaterialFallsBackWhenTheBinaryCannotBeResolved keeps the
// packaged default when the executable resolver fails, so a failing resolver
// cannot render an empty ExecStart (#1437 review).
func TestPanelStateRepairMaterialFallsBackWhenTheBinaryCannotBeResolved(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	material := newPanelStateRepairMaterial(opts, panelStateRepairSnapshot{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "local", Mode: "server"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	}, PlanDependencies{
		Secret:     func(label string) string { return "repair-" + label },
		Executable: func() (string, error) { return "", errors.New("no executable") },
	})

	plan, err := material.Apply(installer.RepairPlan{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	mieru := repairPlanActionContent(t, plan, filepath.Join(opts.SystemdDir, renderer.UnitMieru))
	if !strings.Contains(mieru, "/usr/local/bin/veil") {
		t.Fatalf("unresolvable binary did not fall back to the packaged default:\n%s", mieru)
	}
}

// TestPanelStateRepairMaterialNoWarningWhenTheBinaryIsResolved keeps the normal
// path quiet.
func TestPanelStateRepairMaterialNoWarningWhenTheBinaryIsResolved(t *testing.T) {
	dir := t.TempDir()
	var warnings []string
	material := newPanelStateRepairMaterial(Options{EtcDir: dir, VarDir: dir, SystemdDir: dir}, panelStateRepairSnapshot{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "local", Mode: "server"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	}, PlanDependencies{
		Secret:     func(label string) string { return "repair-" + label },
		Executable: func() (string, error) { return "/opt/veil/bin/veil", nil },
		Notify:     func(msg string) { warnings = append(warnings, msg) },
	})
	if _, err := material.Apply(installer.RepairPlan{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}
