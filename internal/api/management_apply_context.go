package api

type ManagementApplyContext struct {
	state *managementState
}

func NewManagementApplyContext(state *managementState) ManagementApplyContext {
	return ManagementApplyContext{state: state}
}

func (ctx ManagementApplyContext) buildApplyPlanLocked() ApplyPlanResponse {
	s := ctx.state
	return BuildApplyPlan(ApplyPlanInput{
		Settings:                s.settings,
		Inbounds:                s.inbounds,
		Rules:                   s.rules,
		RoutingSource:           s.routingSource,
		Warp:                    s.warp,
		RenderSettingsAvailable: s.hasRenderSettingsLocked(),
		ValidateInboundRender: func(inbound Inbound) error {
			_, err := s.managementConfigRendererLocked().RenderInbound(inbound)
			return err
		},
		ValidateWarpRender: func() error {
			_, err := s.renderWarpConfigLocked()
			return err
		},
	})
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
	return NewPromotedServiceReloader(ctx.state.applyRoot, serviceActionRunner).Reload(liveFiles)
}

func (ctx ManagementApplyContext) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	return NewLiveConfigPromotion(ctx.state.applyRoot, ctx.reloadPromotedServicesLocked).Rollback(records, liveFiles)
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return NewApplyHistoryStore(ctx.state.applyHistoryPathLocked(), maxApplyHistoryEntries).Append(stage, success, response)
}
