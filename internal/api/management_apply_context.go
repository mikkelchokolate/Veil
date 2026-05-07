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
			switch inbound.Protocol {
			case "naiveproxy":
				_, err := s.renderNaiveConfigLocked(inbound)
				return err
			case "hysteria2":
				_, err := s.renderHysteria2ConfigLocked(inbound)
				return err
			default:
				return nil
			}
		},
		ValidateWarpRender: func() error {
			_, err := s.renderWarpConfigLocked()
			return err
		},
	})
}

func (ctx ManagementApplyContext) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	return ctx.state.writeApplyStageLocked(plan)
}

func (ctx ManagementApplyContext) promoteStagedConfigsLocked(stagedPaths []string) ([]string, []string, []livePromotionRecord, error) {
	return ctx.state.promoteStagedConfigsLocked(stagedPaths)
}

func (ctx ManagementApplyContext) reloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	return ctx.state.reloadPromotedServicesLocked(liveFiles)
}

func (ctx ManagementApplyContext) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	return ctx.state.rollbackPromotedConfigsLocked(records, liveFiles)
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return ctx.state.appendApplyHistoryLocked(stage, success, response)
}
