package api

import "github.com/mikkelchokolate/Veil/internal/service"

type ManagementApplyContext struct {
	state *managementState
}

func NewManagementApplyContext(state *managementState) ManagementApplyContext {
	return ManagementApplyContext{state: state}
}

func (ctx ManagementApplyContext) buildApplyPlanLocked() ApplyPlanResponse {
	s := ctx.state
	return NewManagementApplyIntent(ManagementApplyIntentInput{
		ApplyRoot:     s.applyRoot,
		Settings:      s.settings,
		Inbounds:      s.inbounds,
		Rules:         s.rules,
		RoutingSource: s.routingSource,
		Warp:          s.warp,
	}).BuildPlan()
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
	return NewLiveConfigPromotion(ctx.state.applyRoot, ctx.reloadPromotedServicesLocked).Promote(stagedPaths)
}

func (ctx ManagementApplyContext) reloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	return service.NewPromotedServiceReloader(ctx.state.applyRoot, NewManagedRuntimeCatalog(), func(command []string) ServiceActionResult {
		return serviceActionRunner(command)
	}).Reload(liveFiles)
}

func (ctx ManagementApplyContext) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	return NewLiveConfigPromotion(ctx.state.applyRoot, ctx.reloadPromotedServicesLocked).Rollback(records, liveFiles)
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return ctx.state.applyHistoryLocked().Append(stage, success, response)
}
