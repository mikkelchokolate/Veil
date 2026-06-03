package applyflow

import (
	"fmt"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type PromotionRecord struct {
	LivePath    string
	BackupPath  string
	HadPrevious bool
}

type State interface {
	BuildApplyPlanLocked() model.ApplyPlanResponse
	WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error)
	PromoteStagedConfigsLocked([]string) ([]string, []string, []PromotionRecord, error)
	ReloadPromotedServicesLocked([]string) []model.ServiceActionResult
	RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []model.ServiceActionResult)
	AppendApplyHistoryLocked(string, bool, model.ApplyResponse) error
}

type HealthChecker func([]model.ServiceActionResult) []model.ServiceHealthResult

type Workflow struct {
	state       State
	checkHealth HealthChecker
}

func NewWorkflow(state State, checkHealth HealthChecker) Workflow {
	return Workflow{state: state, checkHealth: checkHealth}
}

func (w Workflow) RunLocked(req model.ApplyRequest) (model.ApplyResponse, int, error) {
	s := w.state
	plan := s.BuildApplyPlanLocked()
	if !plan.Valid {
		return model.ApplyResponse{Applied: false, Plan: plan}, http.StatusBadRequest, nil
	}
	if !req.Confirm {
		return model.ApplyResponse{}, http.StatusBadRequest, fmt.Errorf("confirm=true is required to write staged apply files")
	}
	if req.ApplyServices && !req.ApplyLive {
		return model.ApplyResponse{Applied: false, Plan: plan}, http.StatusBadRequest, nil
	}
	written, validations, renderedPaths, err := s.WriteApplyStageLocked(plan)
	if err != nil {
		return model.ApplyResponse{}, http.StatusInternalServerError, err
	}
	response := model.ApplyResponse{Applied: true, Plan: plan, WrittenFiles: written, Validations: validations}
	if req.ApplyLive {
		if err := NewConfigValidationPassPolicy().RequirePassed(validations); err != nil {
			_ = s.AppendApplyHistoryLocked("validation", false, response)
			return response, http.StatusBadRequest, nil
		}
		liveFiles, backupFiles, promotionRecords, err := s.PromoteStagedConfigsLocked(renderedPaths)
		if err != nil {
			return model.ApplyResponse{}, http.StatusInternalServerError, err
		}
		response.LiveApplied = true
		response.LiveFiles = liveFiles
		response.BackupFiles = backupFiles
		if req.ApplyServices {
			serviceActions := s.ReloadPromotedServicesLocked(liveFiles)
			response.ServiceActions = serviceActions
			if err := NewServiceActionSuccessPolicy().RequireSuccessful(serviceActions); err != nil {
				rollbackFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.AppendApplyHistoryLocked("rollback", false, response)
				return response, http.StatusBadRequest, nil
			}
			healthChecks := []model.ServiceHealthResult{}
			if w.checkHealth != nil {
				healthChecks = w.checkHealth(serviceActions)
			}
			response.HealthChecks = healthChecks
			if err := NewServiceHealthPolicy().RequireHealthy(healthChecks); err != nil {
				rollbackFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.AppendApplyHistoryLocked("rollback", false, response)
				return response, http.StatusBadRequest, nil
			}
			response.ServicesApplied = len(serviceActions) > 0
		}
	}
	_ = s.AppendApplyHistoryLocked(HistoryStage(response), true, response)
	return response, http.StatusOK, nil
}

func HistoryStage(response model.ApplyResponse) string {
	switch {
	case response.RolledBack:
		return "rollback"
	case response.ServicesApplied:
		return "services"
	case response.LiveApplied:
		return "live"
	default:
		return "staged"
	}
}
