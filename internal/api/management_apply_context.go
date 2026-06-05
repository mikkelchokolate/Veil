package api

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type ManagementApplyContext struct {
	state *managementState
}

func NewManagementApplyContext(state *managementState) ManagementApplyContext {
	return ManagementApplyContext{state: state}
}

func (ctx ManagementApplyContext) buildApplyPlanLocked() ApplyPlanResponse {
	s := ctx.state
	plan := NewManagementApplyIntent(ManagementApplyIntentInput{
		ApplyRoot:     s.applyRoot,
		Settings:      s.settings,
		Inbounds:      s.inbounds,
		Rules:         s.rules,
		RoutingSource: s.routingSource,
		Warp:          s.warp,
	}).BuildPlan()
	if validation, ok := s.enforceValidationLocked(context.Background(), s.settings, s.inbounds, s.warp); !ok {
		plan.Valid = false
		plan.Issues = append(plan.Issues, validation.Issues...)
		for _, issue := range validation.Issues {
			if issue.Severity == "error" {
				plan.Errors = managementstate.AppendUnique(plan.Errors, issue.Message)
			}
		}
	}
	return plan
}

func (ctx ManagementApplyContext) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	s := ctx.state
	rendered, err := s.renderManagementConfigsLocked()
	if err != nil {
		return nil, nil, nil, err
	}
	return WriteApplyStage(ApplyStageInput{
		ApplyRoot:     s.applyRoot,
		Cipher:        s.cipher,
		Plan:          plan,
		Snapshot:      s.snapshotLocked(),
		Rendered:      rendered,
		RoutingSource: s.routingSource,
		Validate:      stagedConfigValidator,
	})
}

func (ctx ManagementApplyContext) promoteStagedConfigsLocked(stagedPaths []string) ([]string, []string, []livePromotionRecord, error) {
	generatedRoot := filepath.Join(ctx.state.applyRoot, "generated")
	artifactIDs := make([]string, 0, len(stagedPaths))
	for _, stagedPath := range stagedPaths {
		if _, ok := NewLiveConfigPromotion(ctx.state.applyRoot, nil).LivePathForStagedConfig(stagedPath); !ok {
			continue
		}
		relative, err := filepath.Rel(generatedRoot, stagedPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, nil, fmt.Errorf("staged config escapes generated root: %s", stagedPath)
		}
		artifactIDs = append(artifactIDs, filepath.ToSlash(relative))
	}
	activeFiles := make([]string, 0, len(artifactIDs))
	for _, id := range artifactIDs {
		activeFiles = append(activeFiles, filepath.Join(ctx.state.liveRoot, filepath.FromSlash(id)))
	}
	orphans, err := scanLiveConfigOrphans(ctx.state.liveRoot, activeFiles)
	if err != nil {
		return nil, nil, nil, err
	}
	var removeIDs []string
	for _, orphan := range orphans {
		relative, relErr := filepath.Rel(ctx.state.liveRoot, orphan)
		if relErr != nil {
			return nil, nil, nil, relErr
		}
		removeIDs = append(removeIDs, filepath.ToSlash(relative))
	}
	if ctx.state.privileged == nil {
		return nil, nil, nil, fmt.Errorf("privileged helper is unavailable")
	}
	result, err := ctx.state.privileged.Promote(context.Background(), privileged.PromoteRequest{
		ArtifactIDs: artifactIDs, RemoveArtifactIDs: removeIDs,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	liveFiles := livePathsForArtifactIDs(ctx.state.liveRoot, result.WrittenArtifacts)
	records := make([]livePromotionRecord, 0, len(result.WrittenArtifacts)+len(result.RemovedArtifacts))
	for _, id := range append(append([]string{}, result.WrittenArtifacts...), result.RemovedArtifacts...) {
		records = append(records, livePromotionRecord{
			ArtifactID: id,
			BackupID:   result.BackupID,
			LivePath:   filepath.Join(ctx.state.liveRoot, filepath.FromSlash(id)),
		})
	}
	backupFiles := append([]string(nil), result.BackupArtifacts...)
	var orphanedUnits []string
	for _, id := range result.RemovedArtifacts {
		if unit, ok := UnitForArtifactID(id); ok {
			orphanedUnits = append(orphanedUnits, unit)
		}
	}
	ctx.state.orphanedUnits = orphanedUnits
	return liveFiles, backupFiles, records, nil
}

func (ctx ManagementApplyContext) reloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	results := []ServiceActionResult{}
	for _, runtime := range NewManagedRuntimeCatalog().Runtimes() {
		if runtime.PromotedSubpath == "" || runtime.PromotedVerb == "" {
			continue
		}
		want := filepath.Join(ctx.state.liveRoot, filepath.FromSlash(runtime.PromotedSubpath))
		if !containsCleanPath(liveFiles, want) {
			continue
		}
		result := ctx.runPrivilegedServiceAction(runtime.Unit, privileged.ServiceAction(runtime.PromotedVerb))
		results = append(results, result)
		if !result.Success {
			return results
		}
	}
	if len(ctx.state.orphanedUnits) > 0 {
		for _, unit := range ctx.state.orphanedUnits {
			stop := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStop)
			results = append(results, stop)
			if !stop.Success {
				return results
			}
			disable := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionDisable)
			results = append(results, disable)
			if !disable.Success {
				return results
			}
		}
	}
	return results
}

func (ctx ManagementApplyContext) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	if len(records) == 0 || records[0].BackupID == "" || ctx.state.privileged == nil {
		return nil, nil
	}
	result, err := ctx.state.privileged.Promote(context.Background(), privileged.PromoteRequest{
		RestoreBackupID: records[0].BackupID,
	})
	if err != nil {
		return nil, []ServiceActionResult{{
			Name: "promotion-rollback", Command: []string{"helper", "promote", "restore"},
			Success: false, Error: err.Error(),
		}}
	}
	rollbackFiles := livePathsForArtifactIDs(ctx.state.liveRoot, result.WrittenArtifacts)
	rollbackActions := ctx.reloadPromotedServicesLocked(liveFiles)

	liveFilesMap := make(map[string]bool)
	for _, lf := range liveFiles {
		liveFilesMap[filepath.Clean(lf)] = true
	}
	var restoredUnits []string
	for _, record := range records {
		if !liveFilesMap[filepath.Clean(record.LivePath)] {
			if unit, ok := UnitForLiveConfig(record.LivePath); ok {
				restoredUnits = append(restoredUnits, unit)
			}
		}
	}
	if len(restoredUnits) > 0 {
		for _, unit := range restoredUnits {
			rollbackActions = append(rollbackActions,
				ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionEnable),
				ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStart),
			)
		}
	}
	return rollbackFiles, rollbackActions
}

func (ctx ManagementApplyContext) runPrivilegedServiceAction(unit string, action privileged.ServiceAction) ServiceActionResult {
	result := ServiceActionResult{
		Name: unit, Command: []string{"systemctl", string(action), unit},
	}
	if ctx.state.privileged == nil {
		result.Error = "privileged helper is unavailable"
		return result
	}
	if err := ctx.state.privileged.ServiceAction(context.Background(), privileged.ServiceActionRequest{
		Unit: unit, Action: action,
	}); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Success = true
	return result
}

func livePathsForArtifactIDs(liveRoot string, ids []string) []string {
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		paths = append(paths, filepath.Join(liveRoot, filepath.FromSlash(id)))
	}
	sort.Strings(paths)
	return paths
}

func containsCleanPath(paths []string, want string) bool {
	want = filepath.Clean(want)
	for _, path := range paths {
		if filepath.Clean(path) == want {
			return true
		}
	}
	return false
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return ctx.state.applyHistoryLocked().Append(stage, success, response)
}

func (ctx ManagementApplyContext) checkServiceHealthLocked(actions []ServiceActionResult) []ServiceHealthResult {
	if ctx.state.privilegedLocal {
		return service.NewServiceHealthCollection(func(name string) ServiceHealthResult {
			return serviceHealthChecker(name)
		}).Check(actions)
	}
	units := []string{}
	for _, action := range actions {
		if action.Success && action.Name != "" && NewManagedRuntimeCatalog().AllowsHealthUnit(action.Name) {
			units = append(units, action.Name)
		}
	}
	if len(units) == 0 {
		return nil
	}
	statuses, err := ctx.state.privileged.ServiceStatus(context.Background(), privileged.ServiceStatusRequest{Units: units})
	if err != nil {
		return []ServiceHealthResult{{
			Name: "managed-services", Command: []string{"helper", "service-status"},
			Healthy: false, Error: err.Error(),
		}}
	}
	byUnit := make(map[string]privileged.ServiceStatus, len(statuses.Services))
	for _, status := range statuses.Services {
		byUnit[status.Unit] = status
	}
	results := make([]ServiceHealthResult, 0, len(units))
	for _, unit := range units {
		status := byUnit[unit]
		healthy := status.ActiveState == "active" && status.Error == ""
		results = append(results, ServiceHealthResult{
			Name: unit, Command: []string{"helper", "service-status", unit},
			Healthy: healthy, Output: status.ActiveState + "/" + status.SubState, Error: status.Error,
		})
	}
	return results
}
