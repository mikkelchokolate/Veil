package applyflow

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type firewallTransactionalWorkflowState struct {
	events      []string
	reloadOK    bool
	firewallTxn string
	rollbackErr error
}

func (s *firewallTransactionalWorkflowState) BuildApplyPlanLocked() model.ApplyPlanResponse {
	return model.ApplyPlanResponse{Valid: true}
}
func (s *firewallTransactionalWorkflowState) WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error) {
	return nil, []model.ConfigValidationResult{{Name: "config", Valid: true}}, []string{"staged"}, nil
}
func (s *firewallTransactionalWorkflowState) PromoteStagedConfigsLocked([]string) ([]string, []string, []PromotionRecord, error) {
	s.events = append(s.events, "promote")
	return []string{"live"}, nil, []PromotionRecord{{LivePath: "live", BackupPath: "backup", HadPrevious: true}}, nil
}
func (s *firewallTransactionalWorkflowState) ReloadPromotedServicesLocked([]string) []model.ServiceActionResult {
	s.events = append(s.events, "reload")
	return []model.ServiceActionResult{{Name: "runtime", Success: s.reloadOK}}
}
func (s *firewallTransactionalWorkflowState) RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []string, []model.ServiceActionResult) {
	s.events = append(s.events, "rollback-config")
	return []string{"live"}, nil, nil
}
func (*firewallTransactionalWorkflowState) AppendApplyHistoryLocked(string, bool, model.ApplyResponse) error {
	return nil
}
func (s *firewallTransactionalWorkflowState) PrepareFirewallLocked() (string, error) {
	s.events = append(s.events, "firewall-prepare")
	s.firewallTxn = "firewall-tx"
	return s.firewallTxn, nil
}
func (s *firewallTransactionalWorkflowState) CommitFirewallLocked(transactionID string) error {
	if transactionID != s.firewallTxn {
		return errors.New("wrong firewall transaction")
	}
	s.events = append(s.events, "firewall-commit")
	return nil
}
func (s *firewallTransactionalWorkflowState) RollbackFirewallLocked(transactionID string) error {
	if transactionID != s.firewallTxn {
		return errors.New("wrong firewall transaction")
	}
	s.events = append(s.events, "rollback-firewall")
	return s.rollbackErr
}

func TestWorkflowDoesNotIgnoreFirewallRollbackFailure(t *testing.T) {
	state := &firewallTransactionalWorkflowState{reloadOK: false, rollbackErr: errors.New("firewall restore failed")}
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err == nil || status != http.StatusInternalServerError {
		t.Fatalf("firewall rollback failure was hidden: status=%d response=%+v err=%v", status, response, err)
	}
	if response.RolledBack {
		t.Fatalf("partial rollback was reported complete: %+v", response)
	}
}

func TestWorkflowRollsBackPreparedFirewallOnDownstreamFailure(t *testing.T) {
	tests := []struct {
		name       string
		reloadOK   bool
		healthy    bool
		wantEvents []string
	}{
		{name: "service_reload_failure", reloadOK: false, healthy: true,
			wantEvents: []string{"promote", "firewall-prepare", "reload", "rollback-firewall", "rollback-config"}},
		{name: "health_failure", reloadOK: true, healthy: false,
			wantEvents: []string{"promote", "firewall-prepare", "reload", "health", "rollback-firewall", "rollback-config"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &firewallTransactionalWorkflowState{reloadOK: test.reloadOK}
			workflow := NewWorkflow(state, func([]model.ServiceActionResult) []model.ServiceHealthResult {
				state.events = append(state.events, "health")
				return []model.ServiceHealthResult{{Name: "runtime", Healthy: test.healthy}}
			})
			response, status, err := workflow.RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
			if err != nil {
				t.Fatal(err)
			}
			if status != http.StatusBadRequest || response.RolledBack || !response.Ambiguous {
				t.Fatalf("response=%+v status=%d", response, status)
			}
			if !reflect.DeepEqual(state.events, test.wantEvents) {
				t.Fatalf("phase order=%v want=%v", state.events, test.wantEvents)
			}
		})
	}
}

type firewallPrepareFailingState struct {
	firewallTransactionalWorkflowState
	prepareErr error
}

func (s *firewallPrepareFailingState) PrepareFirewallLocked() (string, error) {
	s.events = append(s.events, "firewall-prepare")
	return "", s.prepareErr
}

// #540: a prepare error whose self-rollback failed must NOT claim firewall
// restoration — RollbackComplete/RolledBack stay false and Ambiguous is set.
func TestWorkflowPrepareErrorDoesNotClaimRestorationWhenSelfRollbackFailed(t *testing.T) {
	state := &firewallPrepareFailingState{prepareErr: errors.New("enable ufw: boom; restore previous UFW state: boom")}
	state.reloadOK = true
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if response.FirewallRestored || response.RollbackComplete || response.RolledBack || !response.Ambiguous {
		t.Fatalf("response falsely claimed restoration: %+v", response)
	}
}

// #540: a prepare error before/without a leftover mutation still counts as
// restored — the firewall genuinely is back at its prior state.
func TestWorkflowPrepareErrorAfterCleanSelfRollbackReportsRestored(t *testing.T) {
	state := &firewallPrepareFailingState{prepareErr: errors.New("refusing to enable UFW without a staged SSH or Panel management access rule")}
	state.reloadOK = true
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if !response.FirewallRestored {
		t.Fatalf("expected FirewallRestored for a clean self-rollback, got %+v", response)
	}
}

func TestWorkflowCommitsFirewallOnlyAfterHealthyRuntime(t *testing.T) {
	state := &firewallTransactionalWorkflowState{reloadOK: true}
	workflow := NewWorkflow(state, func([]model.ServiceActionResult) []model.ServiceHealthResult {
		state.events = append(state.events, "health")
		return []model.ServiceHealthResult{{Name: "runtime", Healthy: true}}
	})
	_, status, err := workflow.RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	want := []string{"promote", "firewall-prepare", "reload", "health", "firewall-commit"}
	if !reflect.DeepEqual(state.events, want) {
		t.Fatalf("phase order=%v want=%v", state.events, want)
	}
}

// firewallPrepareErrorState fails in PrepareFirewallLocked; prepareErr
// controls whether the prepare's own self-rollback reported failure via the
// "restore previous UFW state" marker the apply paths embed (#540).
type firewallPrepareErrorState struct {
	firewallTransactionalWorkflowState
	prepareErr error
}

func (s *firewallPrepareErrorState) PrepareFirewallLocked() (string, error) {
	s.events = append(s.events, "firewall-prepare")
	return "", s.prepareErr
}

// #540: a prepare error whose self-rollback failed must not claim firewall
// restoration — the firewall may still carry the half-applied ruleset.
func TestWorkflowPrepareFirewallErrorDoesNotClaimRestoreWhenSelfRollbackFailed(t *testing.T) {
	state := &firewallPrepareErrorState{
		prepareErr: errors.New("ufw reload failed; restore previous UFW state: command timed out"),
	}
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err == nil || status != http.StatusInternalServerError {
		t.Fatalf("prepare failure must surface: status=%d err=%v", status, err)
	}
	if response.FirewallRestored {
		t.Fatalf("failed self-rollback reported FirewallRestored: %+v", response)
	}
	if response.RollbackComplete || response.RolledBack || !response.Ambiguous {
		t.Fatalf("unproven firewall restore was reported complete: %+v", response)
	}
}

// #540: when the failed prepare's own self-rollback completed (no
// restore-failure marker in the error), firewall restoration may be claimed.
func TestWorkflowPrepareFirewallErrorCleanSelfRollbackMayClaimRestore(t *testing.T) {
	state := &firewallPrepareErrorState{
		prepareErr: errors.New("ufw dry-run rejected rule"),
	}
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err == nil || status != http.StatusInternalServerError {
		t.Fatalf("prepare failure must surface: status=%d err=%v", status, err)
	}
	if !response.FirewallRestored {
		t.Fatalf("clean self-rollback must report FirewallRestored: %+v", response)
	}
	// Artifacts restored and nothing service-side ran: the rollback is
	// complete, so the job is terminal failed rather than recovery_pending.
	if !response.RollbackComplete || !response.RolledBack || response.Ambiguous {
		t.Fatalf("clean rollback was reported ambiguous: %+v", response)
	}
}
