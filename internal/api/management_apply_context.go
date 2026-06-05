package api

import (
	"context"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
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
	liveFiles, backupFiles, records, err := NewLiveConfigPromotion(ctx.state.applyRoot, ctx.reloadPromotedServicesLocked).Promote(stagedPaths)
	if err != nil {
		return nil, nil, nil, err
	}
	liveFilesMap := make(map[string]bool)
	for _, lf := range liveFiles {
		liveFilesMap[filepath.Clean(lf)] = true
	}
	var orphanedUnits []string
	for _, record := range records {
		if !liveFilesMap[filepath.Clean(record.LivePath)] {
			if unit, ok := UnitForLiveConfig(record.LivePath); ok {
				orphanedUnits = append(orphanedUnits, unit)
			}
		}
	}
	ctx.state.orphanedUnits = orphanedUnits
	return liveFiles, backupFiles, records, nil
}

func (ctx ManagementApplyContext) reloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	reloader := service.NewPromotedServiceReloader(ctx.state.applyRoot, NewManagedRuntimeCatalog(), func(command []string) ServiceActionResult {
		return serviceActionRunner(command)
	})
	results := reloader.Reload(liveFiles)

	if len(ctx.state.orphanedUnits) > 0 {
		for _, unit := range ctx.state.orphanedUnits {
			stopCmd := []string{"systemctl", "stop", unit}
			stopRes := serviceActionRunner(stopCmd)
			if stopRes.Name == "" {
				stopRes.Name = unit
			}
			results = append(results, stopRes)

			disableCmd := []string{"systemctl", "disable", unit}
			disableRes := serviceActionRunner(disableCmd)
			if disableRes.Name == "" {
				disableRes.Name = unit
			}
			results = append(results, disableRes)
		}
	}
	return results
}

func (ctx ManagementApplyContext) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	rollbackFiles, rollbackActions := NewLiveConfigPromotion(ctx.state.applyRoot, ctx.reloadPromotedServicesLocked).Rollback(records, liveFiles)

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
			enableCmd := []string{"systemctl", "enable", unit}
			enableRes := serviceActionRunner(enableCmd)
			if enableRes.Name == "" {
				enableRes.Name = unit
			}
			rollbackActions = append(rollbackActions, enableRes)

			startCmd := []string{"systemctl", "start", unit}
			startRes := serviceActionRunner(startCmd)
			if startRes.Name == "" {
				startRes.Name = unit
			}
			rollbackActions = append(rollbackActions, startRes)
		}
	}
	return rollbackFiles, rollbackActions
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return ctx.state.applyHistoryLocked().Append(stage, success, response)
}
