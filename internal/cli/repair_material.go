package cli

import (
	"path/filepath"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/renderer"
)

type panelStateRepairSnapshot struct {
	Settings api.Settings
	Inbounds []api.Inbound
	Rules    []api.RoutingRule
	Warp     api.WarpConfig
}

type panelStateRepairMaterial struct {
	opts     repairWorkflowOptions
	snapshot panelStateRepairSnapshot
}

func newPanelStateRepairMaterial(opts repairWorkflowOptions, snapshot panelStateRepairSnapshot) panelStateRepairMaterial {
	return panelStateRepairMaterial{opts: opts, snapshot: snapshot}
}

func (m panelStateRepairMaterial) Apply(plan installer.RepairPlan) (installer.RepairPlan, error) {
	snapshot := m.snapshot
	if err := applyPanelSettingsRepairActions(&plan, m.opts, snapshot.Settings); err != nil {
		return installer.RepairPlan{}, err
	}
	if err := m.addGeneratedConfigActions(&plan); err != nil {
		return installer.RepairPlan{}, err
	}
	if err := m.addRuntimeUnitActions(&plan); err != nil {
		return installer.RepairPlan{}, err
	}
	return plan, nil
}

func (m panelStateRepairMaterial) addGeneratedConfigActions(plan *installer.RepairPlan) error {
	snapshot := m.snapshot
	configs, err := api.BuildGeneratedConfigSet(api.GeneratedConfigInput{ApplyRoot: m.opts.EtcDir, Settings: snapshot.Settings, Inbounds: snapshot.Inbounds, Rules: snapshot.Rules, Warp: snapshot.Warp})
	if err != nil {
		return err
	}
	for path, body := range configs {
		if err := addRepairFileAction(plan, path, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (m panelStateRepairMaterial) addRuntimeUnitActions(plan *installer.RepairPlan) error {
	if m.opts.SystemdDir == "" {
		return nil
	}
	snapshot := m.snapshot
	units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: m.opts.EtcDir, CaddyBinary: resolvedRepairBinaryPath("caddy")})
	unitNames := runtimeUnitNamesForState(snapshot.Inbounds, snapshot.Warp)
	if snapshot.Settings.PanelAccess == "caddy" {
		unitNames = appendRepairUnit(unitNames, renderer.UnitNaive)
	}
	for _, unitName := range unitNames {
		body := units[unitName]
		if body == "" {
			continue
		}
		if err := addRepairFileAction(plan, filepath.Join(m.opts.SystemdDir, unitName), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}
