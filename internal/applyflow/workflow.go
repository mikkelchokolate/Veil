package applyflow

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []string, []model.ServiceActionResult)
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
					rollbackFiles, removedFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
					response.RollbackFiles = rollbackFiles
					response.RollbackActions = rollbackActions
					response.ArtifactsRestored = !response.ArtifactsChanged || len(rollbackFiles) > 0 || len(removedFiles) > 0
					// On this path the firewall prepare failed before any
					// service reload ran, so no unit ever loaded the promoted
					// config — an empty rollback action set genuinely means
					// "nothing service-side to undo", not a vacuous success.
					// Failed rollback actions still report false (#540).
					response.ServicesRestored = allServiceActionsSuccessful(rollbackActions)
					// Both prepare implementations roll their own mutation back
					// on error (ApplySafely restore / journaled rollback); claim
					// firewall restoration only when that self-rollback did not
					// itself report failure (#540).
					response.FirewallRestored = !firewallSelfRestoreFailed(err)
					rollbackHealthOK := w.recordRollbackHealth(&response, rollbackActions)
					response.RollbackComplete = response.ArtifactsRestored && response.ServicesRestored &&
						response.FirewallRestored && rollbackHealthOK
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
				rollbackFiles, removedFiles, rollbackActions := s.RollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				// A first-create config leaves no restored file: its artifact is
				// removed by the restore instead. Count successful deletions toward
				// restoration so a clean first-inbound rollback is not stranded as
				// recovery_pending (audit #343).
				response.ArtifactsRestored = !response.ArtifactsChanged || len(rollbackFiles) > 0 || len(removedFiles) > 0
				response.ServicesRestored = !response.ServicesChanged ||
					(len(rollbackActions) > 0 && allServiceActionsSuccessful(rollbackActions))
				rollbackHealthOK := w.recordRollbackHealth(&response, rollbackActions)
				response.RollbackComplete = response.ArtifactsRestored && response.ServicesRestored &&
					response.FirewallRestored && rollbackHealthOK && rollbackErr == nil
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
			healthErr := NewServiceHealthPolicy().RequireHealthy(healthChecks)
			if healthErr == nil && len(healthChecks) == 0 && requiresServiceHealthEvidence(serviceActions) {
				// A unit was (re)started but produced zero health probes — an
				// empty check set is not convergence evidence (#541).
				healthErr = errors.New("activated services produced no health evidence")
			}
			if healthErr != nil {
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
			// Applied is runtime-convergence evidence (#542): either service
			// actions ran and converged the units, or the plan contains no
			// service operation at all — promotion alone converges artifacts
			// that no runtime unit reloads (e.g. subscription snapshots). A
			// revision whose plan expects reload_service/restart_service work
			// but produced no action is NOT marked applied, so the durable
			// layer keeps desired>applied instead of claiming convergence.
			response.Applied = response.ServicesApplied || !planExpectsServiceWork(plan)
		}
	}
	if err := s.AppendApplyHistoryLocked(HistoryStage(response), true, response); err != nil {
		return response, http.StatusInternalServerError, fmt.Errorf("persist apply history: %w", err)
	}
	return response, http.StatusOK, nil
}

// firewallSelfRestoreFailed reports whether a failed firewall prepare left the
// firewall mutated: the apply paths embed a restore-failure marker in the
// returned error when their self-rollback could not complete (#540).
func firewallSelfRestoreFailed(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "restore previous UFW state") || strings.Contains(msg, "firewall rollback failed")
}

// planExpectsServiceWork reports whether the plan declares any service
// lifecycle operation (reload_service, restart_service, …). Those operations
// are the plan's evidence that promoted artifacts must be loaded into a
// running unit — when they exist, convergence requires the service actions to
// have actually run (#542).
func planExpectsServiceWork(plan model.ApplyPlanResponse) bool {
	for _, op := range plan.Operations {
		if strings.HasSuffix(op.Type, "_service") {
			return true
		}
	}
	return false
}

// requiresServiceHealthEvidence reports whether the action set contains a
// (re)activation — systemctl start/restart/reload or a Caddy Admin API load —
// whose target unit must be observed healthy afterwards. Stop/disable/enable
// and bookkeeping actions need no health probe (#541).
func requiresServiceHealthEvidence(actions []model.ServiceActionResult) bool {
	for _, action := range actions {
		if !action.Success {
			continue
		}
		if len(action.Command) >= 2 && action.Command[0] == "systemctl" {
			switch action.Command[1] {
			case "start", "restart", "reload":
				return true
			}
		}
		if len(action.Command) >= 3 && action.Command[0] == "caddy" && action.Command[1] == "admin" && action.Command[2] == "load" {
			return true
		}
	}
	return false
}

// recordRollbackHealth runs post-rollback health probes and reports whether
// the health evidence is satisfied. PostRollbackHealthPass is strictly
// evidence-based: it is false whenever no check ran (#541). The returned bool
// is what feeds RollbackComplete: health is satisfied when probes passed, or
// when the rollback never re-activated a unit and produced nothing to probe —
// a pure stop/disable teardown must still be able to complete (#343). When
// activation actions ran but produced zero checks, or when checks ran and any
// failed, health is NOT satisfied and the rollback stays ambiguous.
func (w Workflow) recordRollbackHealth(response *model.ApplyResponse, rollbackActions []model.ServiceActionResult) bool {
	var checks []model.ServiceHealthResult
	if w.checkHealth != nil && len(rollbackActions) > 0 {
		checks = w.checkHealth(rollbackActions)
	}
	response.PostRollbackHealthPass = len(checks) > 0 &&
		NewServiceHealthPolicy().RequireHealthy(checks) == nil
	return response.PostRollbackHealthPass ||
		(!requiresServiceHealthEvidence(rollbackActions) && len(checks) == 0)
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
