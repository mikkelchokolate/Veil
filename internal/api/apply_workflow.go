package api

import (
	"github.com/veil-panel/veil/internal/applyflow"
	"github.com/veil-panel/veil/internal/service"
)

type applyWorkflowState interface {
	buildApplyPlanLocked() ApplyPlanResponse
	writeApplyStageLocked(ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error)
	promoteStagedConfigsLocked([]string) ([]string, []string, []livePromotionRecord, error)
	reloadPromotedServicesLocked([]string) []ServiceActionResult
	rollbackPromotedConfigsLocked([]livePromotionRecord, []string) ([]string, []ServiceActionResult)
	appendApplyHistoryLocked(string, bool, ApplyResponse) error
}

type ApplyWorkflow struct {
	state applyWorkflowState
}

func NewApplyWorkflow(state applyWorkflowState) ApplyWorkflow {
	return ApplyWorkflow{state: state}
}

func (w ApplyWorkflow) RunLocked(req ApplyRequest) (ApplyResponse, int, error) {
	return applyflow.NewWorkflow(applyWorkflowStateAdapter(w), func(actions []ServiceActionResult) []ServiceHealthResult {
		return service.NewServiceHealthCollection(func(name string) ServiceHealthResult {
			return serviceHealthChecker(name)
		}).Check(actions)
	}).RunLocked(req)
}

type applyWorkflowStateAdapter struct {
	state applyWorkflowState
}

func (a applyWorkflowStateAdapter) BuildApplyPlanLocked() ApplyPlanResponse {
	return a.state.buildApplyPlanLocked()
}

func (a applyWorkflowStateAdapter) WriteApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	return a.state.writeApplyStageLocked(plan)
}

func (a applyWorkflowStateAdapter) PromoteStagedConfigsLocked(paths []string) ([]string, []string, []applyflow.PromotionRecord, error) {
	return a.state.promoteStagedConfigsLocked(paths)
}

func (a applyWorkflowStateAdapter) ReloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	return a.state.reloadPromotedServicesLocked(liveFiles)
}

func (a applyWorkflowStateAdapter) RollbackPromotedConfigsLocked(records []applyflow.PromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	return a.state.rollbackPromotedConfigsLocked(records, liveFiles)
}

func (a applyWorkflowStateAdapter) AppendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return a.state.appendApplyHistoryLocked(stage, success, response)
}
