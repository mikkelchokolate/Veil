package api

import (
	"fmt"
	"net/http"
)

type ApplyWorkflow struct {
	state *managementState
}

func NewApplyWorkflow(state *managementState) ApplyWorkflow {
	return ApplyWorkflow{state: state}
}

func (w ApplyWorkflow) RunLocked(req ApplyRequest) (ApplyResponse, int, error) {
	s := w.state
	plan := s.buildApplyPlanLocked()
	if !plan.Valid {
		return ApplyResponse{Applied: false, Plan: plan}, http.StatusBadRequest, nil
	}
	if !req.Confirm {
		return ApplyResponse{}, http.StatusBadRequest, fmt.Errorf("confirm=true is required to write staged apply files")
	}
	if req.ApplyServices && !req.ApplyLive {
		return ApplyResponse{Applied: false, Plan: plan}, http.StatusBadRequest, nil
	}
	written, validations, renderedPaths, err := s.writeApplyStageLocked(plan)
	if err != nil {
		return ApplyResponse{}, http.StatusInternalServerError, err
	}
	response := ApplyResponse{Applied: true, Plan: plan, WrittenFiles: written, Validations: validations}
	if req.ApplyLive {
		if err := requirePassedValidations(validations); err != nil {
			_ = s.appendApplyHistoryLocked("validation", false, response)
			return response, http.StatusBadRequest, nil
		}
		liveFiles, backupFiles, promotionRecords, err := s.promoteStagedConfigsLocked(renderedPaths)
		if err != nil {
			return ApplyResponse{}, http.StatusInternalServerError, err
		}
		response.LiveApplied = true
		response.LiveFiles = liveFiles
		response.BackupFiles = backupFiles
		if req.ApplyServices {
			serviceActions := s.reloadPromotedServicesLocked(liveFiles)
			response.ServiceActions = serviceActions
			if err := requireSuccessfulServiceActions(serviceActions); err != nil {
				rollbackFiles, rollbackActions := s.rollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.appendApplyHistoryLocked("rollback", false, response)
				return response, http.StatusBadRequest, nil
			}
			healthChecks := checkServiceHealth(serviceActions)
			response.HealthChecks = healthChecks
			if err := requireHealthyServices(healthChecks); err != nil {
				rollbackFiles, rollbackActions := s.rollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.appendApplyHistoryLocked("rollback", false, response)
				return response, http.StatusBadRequest, nil
			}
			response.ServicesApplied = len(serviceActions) > 0
		}
	}
	_ = s.appendApplyHistoryLocked(applyHistoryStage(response), true, response)
	return response, http.StatusOK, nil
}
