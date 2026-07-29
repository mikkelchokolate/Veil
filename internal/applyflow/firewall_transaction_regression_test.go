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
func (s *firewallTransactionalWorkflowState) RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []model.ServiceActionResult) {
	s.events = append(s.events, "rollback-config")
	return []string{"live"}, nil
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
	return nil
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
			if status != http.StatusBadRequest || !response.RolledBack {
				t.Fatalf("response=%+v status=%d", response, status)
			}
			if !reflect.DeepEqual(state.events, test.wantEvents) {
				t.Fatalf("phase order=%v want=%v", state.events, test.wantEvents)
			}
		})
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
