package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/renderer"
	"github.com/veil-panel/veil/internal/secrets"
)

func buildRepairPlanFromOptions(opts repairWorkflowOptions) (installer.RepairPlan, error) {
	built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Secret: randomSecret,
	})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	veilBinary, executableErr := installExecutableFunc()
	if executableErr != nil {
		veilBinary = ""
	}
	plan, err := installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, VeilBinary: veilBinary, CaddyBinary: resolvedRepairBinaryPath("caddy")})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	return addPanelStateRepairActions(plan, opts)
}

func addPanelStateRepairActions(plan installer.RepairPlan, opts repairWorkflowOptions) (installer.RepairPlan, error) {
	statePath := filepath.Join(opts.VarDir, "state.json")
	if _, err := os.Stat(statePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return plan, nil
		}
		return installer.RepairPlan{}, err
	}
	store := api.NewStateStore(statePath, repairStateCipher(filepath.Join(opts.EtcDir, "state.key")))
	snapshot, ok, err := store.Load()
	if err != nil {
		return installer.RepairPlan{}, err
	}
	if !ok {
		return plan, nil
	}
	configs, err := api.BuildGeneratedConfigSet(api.GeneratedConfigInput{ApplyRoot: opts.EtcDir, Settings: snapshot.Settings, Inbounds: snapshot.Inbounds, Rules: snapshot.Rules, Warp: snapshot.Warp})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	for path, body := range configs {
		if err := addRepairFileAction(&plan, path, body, 0o600); err != nil {
			return installer.RepairPlan{}, err
		}
	}
	units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: opts.EtcDir, CaddyBinary: resolvedRepairBinaryPath("caddy")})
	for _, unitName := range runtimeUnitNamesForState(snapshot.Inbounds, snapshot.Warp) {
		body := units[unitName]
		if body == "" || opts.SystemdDir == "" {
			continue
		}
		if err := addRepairFileAction(&plan, filepath.Join(opts.SystemdDir, unitName), body, 0o644); err != nil {
			return installer.RepairPlan{}, err
		}
	}
	return plan, nil
}

func resolvedRepairBinaryPath(name string) string {
	path, err := commandLookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func repairStateCipher(keyPath string) *secrets.Cipher {
	if _, err := os.Stat(keyPath); err != nil {
		return nil
	}
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		return nil
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return nil
	}
	return cipher
}

func runtimeUnitNamesForState(inbounds []api.Inbound, warp api.WarpConfig) []string {
	return api.NewProtocolRuntimeProvisioning().Plan(inbounds, warp).SystemdUnits()
}

func addRepairFileAction(plan *installer.RepairPlan, path string, content string, mode os.FileMode) error {
	for _, action := range plan.Actions {
		if action.Path == path {
			return nil
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			plan.Actions = append(plan.Actions, installer.RepairAction{Path: path, Reason: installer.RepairReasonMissing, Content: content, Mode: mode})
			return nil
		}
		return err
	}
	if string(body) != content {
		plan.Actions = append(plan.Actions, installer.RepairAction{Path: path, Reason: installer.RepairReasonDrifted, Content: content, Mode: mode})
	}
	return nil
}
