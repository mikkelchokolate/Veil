package applyflow

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type PromotionRecord struct {
	LivePath    string
	BackupPath  string
	HadPrevious bool
	ArtifactID  string
	BackupID    string
}

type State interface {
	BuildApplyPlanLocked() model.ApplyPlanResponse
	WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error)
	PromoteStagedConfigsLocked([]string) ([]string, []string, []PromotionRecord, error)
	ReloadPromotedServicesLocked([]string) []model.ServiceActionResult
	RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []model.ServiceActionResult)
	AppendApplyHistoryLocked(string, bool, model.ApplyResponse) error
}

type FirewallTransactionState interface {
	PrepareFirewallLocked() (string, error)
	CommitFirewallLocked(string) error
	RollbackFirewallLocked(string) error
}

type PublicationPhaseState interface {
	AdvancePublicationPhaseLocked(string) error
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
	advancePhase := func(phase string) error {
		if state, ok := s.(PublicationPhaseState); ok {
			return state.AdvancePublicationPhaseLocked(phase)
		}
		return nil
	}
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
	written, validations, _, err := s.WriteApplyStageLocked(plan)
	if err != nil {
		return model.ApplyResponse{}, http.StatusInternalServerError, err
	}
	response := model.ApplyResponse{Applied: false, Plan: plan, WrittenFiles: written, Validations: validations}
	if req.ApplyLive {
		if err := NewConfigValidationPassPolicy().RequirePassed(validations); err != nil {
			if historyErr := s.AppendApplyHistoryLocked("validation", false, response); historyErr != nil {
				return response, http.StatusInternalServerError, errors.Join(err, historyErr)
			}
			return response, http.StatusBadRequest, nil
		}
		liveFiles, backupFiles, promotionRecords, err := s.PromoteStagedConfigsLocked(written)
		if err != nil {
			response.MutationStarted = true
			response.Ambiguous = true
			return response, http.StatusInternalServerError, err
		}
		// Report LiveApplied only when at least one file was actually promoted;
		// an idempotent no-op apply must not claim a live change happened.
		response.LiveApplied = len(liveFiles) > 0
		response.LiveFiles = liveFiles
		response.BackupFiles = backupFiles
		response.MutationStarted = len(liveFiles) > 0
		response.ArtifactsChanged = len(liveFiles) > 0
		if req.ApplyServices {
			firewallState, hasFirewallTransaction := s.(FirewallTransactionState)
			firewallTransactionID := ""
			if hasFirewallTransaction {
				firewallTransactionID, err = firewallState.PrepareFirewallLocked()
				if err != nil {
					rollbackFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
					response.RollbackFiles = rollbackFiles
					response.RollbackActions = rollbackActions
					response.ArtifactsRestored = !response.ArtifactsChanged || len(rollbackFiles) > 0
					response.ServicesRestored = allServiceActionsSuccessful(rollbackActions)
					response.FirewallRestored = true
					response.PostRollbackHealthPass = true
					response.RollbackComplete = response.ArtifactsRestored && response.ServicesRestored
					response.RolledBack = response.RollbackComplete
					response.Ambiguous = !response.RollbackComplete
					if historyErr := s.AppendApplyHistoryLocked("rollback", false, response); historyErr != nil {
						return response, http.StatusInternalServerError, errors.Join(err, historyErr)
					}
					return response, http.StatusInternalServerError, err
				}
			}
			rollbackRuntime := func() error {
				var rollbackErr error
				if hasFirewallTransaction && firewallTransactionID != "" {
					if err := firewallState.RollbackFirewallLocked(firewallTransactionID); err != nil {
						rollbackErr = fmt.Errorf("rollback firewall transaction: %w", err)
					} else {
						response.FirewallRestored = true
					}
				} else {
					response.FirewallRestored = true
				}
				rollbackFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				response.ArtifactsRestored = !response.ArtifactsChanged || len(rollbackFiles) > 0
				response.ServicesRestored = !response.ServicesChanged ||
					(len(rollbackActions) > 0 && allServiceActionsSuccessful(rollbackActions))
				response.PostRollbackHealthPass = true
				if response.ServicesChanged && w.checkHealth != nil && len(rollbackActions) > 0 {
					response.PostRollbackHealthPass = NewServiceHealthPolicy().RequireHealthy(w.checkHealth(rollbackActions)) == nil
				}
				response.RollbackComplete = response.ArtifactsRestored && response.ServicesRestored && response.FirewallRestored && response.PostRollbackHealthPass && rollbackErr == nil
				response.RolledBack = response.RollbackComplete
				response.Ambiguous = !response.RollbackComplete
				if historyErr := s.AppendApplyHistoryLocked("rollback", false, response); historyErr != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("persist rollback history: %w", historyErr))
				}
				return rollbackErr
			}
			if err := advancePhase("services_planned"); err != nil {
				rollbackErr := rollbackRuntime()
				return response, http.StatusInternalServerError, errors.Join(fmt.Errorf("persist services-planned publication phase: %w", err), rollbackErr)
			}
			serviceActions := s.ReloadPromotedServicesLocked(liveFiles)
			response.ServiceActions = serviceActions
			response.ServicesChanged = len(serviceActions) > 0
			response.MutationStarted = response.MutationStarted || response.ServicesChanged
			if err := NewServiceActionSuccessPolicy().RequireSuccessful(serviceActions); err != nil {
				if rollbackErr := rollbackRuntime(); rollbackErr != nil {
					return response, http.StatusInternalServerError, rollbackErr
				}
				return response, http.StatusBadRequest, nil
			}
			if err := advancePhase("services_converged"); err != nil {
				rollbackErr := rollbackRuntime()
				return response, http.StatusInternalServerError, errors.Join(fmt.Errorf("persist services-converged publication phase: %w", err), rollbackErr)
			}
			healthChecks := []model.ServiceHealthResult{}
			if w.checkHealth != nil {
				healthChecks = w.checkHealth(serviceActions)
			}
			response.HealthChecks = healthChecks
			if err := NewServiceHealthPolicy().RequireHealthy(healthChecks); err != nil {
				if rollbackErr := rollbackRuntime(); rollbackErr != nil {
					return response, http.StatusInternalServerError, rollbackErr
				}
				return response, http.StatusBadRequest, nil
			}
			if err := advancePhase("health_verified"); err != nil {
				rollbackErr := rollbackRuntime()
				return response, http.StatusInternalServerError, errors.Join(fmt.Errorf("persist health-verified publication phase: %w", err), rollbackErr)
			}
			if hasFirewallTransaction && firewallTransactionID != "" {
				if err := firewallState.CommitFirewallLocked(firewallTransactionID); err != nil {
					response.FirewallChanged = true
					response.MutationStarted = true
					response.Ambiguous = true
					if rollbackErr := rollbackRuntime(); rollbackErr != nil {
						return response, http.StatusInternalServerError, errors.Join(err, rollbackErr)
					}
					return response, http.StatusInternalServerError, err
				}
				response.FirewallChanged = true
				response.MutationStarted = true
				if err := advancePhase("firewall_committed"); err != nil {
					rollbackErr := rollbackRuntime()
					return response, http.StatusInternalServerError, errors.Join(fmt.Errorf("persist firewall-committed publication phase: %w", err), rollbackErr)
				}
			}
			response.ServicesApplied = len(serviceActions) > 0
			response.Applied = true
		}
	}
	if err := s.AppendApplyHistoryLocked(HistoryStage(response), true, response); err != nil {
		return response, http.StatusInternalServerError, fmt.Errorf("persist apply history: %w", err)
	}
	return response, http.StatusOK, nil
}

func allServiceActionsSuccessful(actions []model.ServiceActionResult) bool {
	for _, action := range actions {
		if !action.Success {
			return false
		}
	}
	return true
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
