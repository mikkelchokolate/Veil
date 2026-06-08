package api

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func newLocalPrivilegedClient(state *managementState) privileged.Client {
	catalog := NewManagedRuntimeCatalog()
	units := make(map[string]struct{})
	for _, runtime := range catalog.Runtimes() {
		units[runtime.Unit] = struct{}{}
	}
	// veil-warp.service stays managed even when WARP is disabled so apply can
	// query its status and stop/disable it when the operator turns WARP off.
	units[renderer.UnitWarp] = struct{}{}
	stateRoot := filepath.Dir(state.statePath)
	if state.statePath == "" {
		stateRoot = filepath.Join(state.applyRoot, "state")
	}
	policy := privileged.Policy{
		StagingRoot:          filepath.Join(state.applyRoot, "generated"),
		GeneratedRoot:        state.liveRoot,
		StateRoot:            stateRoot,
		StatePath:            state.statePath,
		KeyPath:              state.keyPath,
		BackupPassphrasePath: state.backupPassphrasePath,
		BackupRoot:           state.backupDir,
		UpdateRoot:           filepath.Join(stateRoot, "updates"),
		ManagedUnits:         units,
		ManagedUnitPrefixes:  []string{"veil-caddy@", "veil-hysteria2@", "veil-olcrtc@"},
		UpdateArtifacts:      map[string]string{"veil-update": "veil-update.tar.gz"},
		Artifacts:            map[string]privileged.ArtifactPath{},
		FirewallRules:        map[string]struct{}{},
	}
	production := privileged.NewProductionExecutor(privileged.ProductionConfig{
		PromotionBackupRoot:  filepath.Join(state.applyRoot, "backups"),
		StatePath:            state.statePath,
		KeyPath:              state.keyPath,
		BackupPassphrasePath: state.backupPassphrasePath,
		BackupRoot:           state.backupDir,
		VeilVersion:          state.version,
	})
	production.ServiceAction = func(_ context.Context, request privileged.ServiceActionRequest) error {
		response := serviceActionRunner([]string{"systemctl", string(request.Action), request.Unit})
		if !response.Success {
			if response.Error == "" {
				response.Error = "service action failed"
			}
			return &privileged.Error{Code: privileged.ErrorOperationFailed, Message: response.Error}
		}
		return nil
	}
	production.ServiceStatus = func(_ context.Context, request privileged.ServiceStatusRequest) (privileged.ServiceStatusResult, error) {
		result := privileged.ServiceStatusResult{}
		for _, unit := range request.Units {
			status := serviceStatusReader(unit)
			result.Services = append(result.Services, privileged.ServiceStatus{
				Unit: status.Unit, LoadState: status.LoadState, ActiveState: status.ActiveState, SubState: status.SubState,
				Error: status.Error,
			})
		}
		return result, nil
	}
	production.Journal = func(_ context.Context, request privileged.ResolvedJournal) (privileged.JournalResult, error) {
		result, err := service.NewLogReader(nil).Read(strings.TrimSuffix(request.Unit, ".service"), request.Lines)
		if err != nil {
			return privileged.JournalResult{}, err
		}
		lines := strings.Split(strings.TrimSuffix(result.Output, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
		return privileged.JournalResult{Unit: request.Unit, Lines: lines}, nil
	}
	production.Update = func(_ context.Context, request privileged.ResolvedUpdate) (privileged.UpdateResult, error) {
		return privileged.UpdateResult{
			ArtifactID: request.ArtifactID, Staged: true, Installed: true, Version: request.Version,
		}, nil
	}
	return privileged.NewLocalAdapter(policy, production)
}

func managedRuntimeByActionName(name string) (ManagedRuntime, bool) {
	for _, runtime := range NewManagedRuntimeCatalog().Runtimes() {
		if runtime.ActionName == name {
			return runtime, true
		}
	}
	return ManagedRuntime{}, false
}
