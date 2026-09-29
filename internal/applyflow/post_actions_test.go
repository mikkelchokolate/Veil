package applyflow

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// postActionsWorkflowState exercises the PostServiceActionsState extension:
// a failing post-policy action (the panel IP certificate issuance, #1169)
// must be reported but must never roll a converged apply back.
type postActionsWorkflowState struct {
	firewallTransactionalWorkflowState
	postActions      []model.ServiceActionResult
	postActionsCalls int
}

func (s *postActionsWorkflowState) PostServiceActionsLocked([]string) []model.ServiceActionResult {
	s.postActionsCalls++
	s.events = append(s.events, "post-actions")
	return s.postActions
}

func TestWorkflowPostActionsFailureDoesNotRollback(t *testing.T) {
	state := &postActionsWorkflowState{
		postActions: []model.ServiceActionResult{
			{Name: "issue-ip-cert", Success: false, Error: "ACME validation failed"},
		},
	}
	state.reloadOK = true
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err != nil || status != http.StatusOK {
		t.Fatalf("a failed post-policy action must not fail the apply: status=%d err=%v", status, err)
	}
	if !response.Applied || response.RolledBack || response.Ambiguous {
		t.Fatalf("apply was damaged by a best-effort action failure: %+v", response)
	}
	// The failed issuance is still reported to the caller.
	var found bool
	for _, action := range response.ServiceActions {
		if action.Name == "issue-ip-cert" && !action.Success && action.Error != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed post action missing from ServiceActions: %+v", response.ServiceActions)
	}
	if state.postActionsCalls != 1 {
		t.Fatalf("post actions ran %d times", state.postActionsCalls)
	}
}

func TestWorkflowPostActionsRunAfterConvergenceProven(t *testing.T) {
	state := &postActionsWorkflowState{
		postActions: []model.ServiceActionResult{{Name: "issue-ip-cert", Success: true}},
	}
	state.reloadOK = true
	workflow := NewWorkflow(state, func([]model.ServiceActionResult) []model.ServiceHealthResult {
		state.events = append(state.events, "health")
		return []model.ServiceHealthResult{{Name: "runtime", Healthy: true}}
	})
	_, status, err := workflow.RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	want := []string{"promote", "firewall-prepare", "reload", "health", "firewall-commit", "post-actions"}
	if !reflect.DeepEqual(state.events, want) {
		t.Fatalf("post actions must run after policy+health+firewall commit: %v", state.events)
	}
}

func TestWorkflowPostActionsSkippedOnRollback(t *testing.T) {
	state := &postActionsWorkflowState{
		postActions: []model.ServiceActionResult{{Name: "issue-ip-cert", Success: true}},
	}
	state.reloadOK = false
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status == http.StatusOK {
		t.Fatalf("failed service action must still roll back: %+v", response)
	}
	if state.postActionsCalls != 0 {
		t.Fatal("post actions must not run when the service policy failed")
	}
	for _, action := range response.ServiceActions {
		if action.Name == "issue-ip-cert" {
			t.Fatalf("post action leaked into the rolled-back response: %+v", response.ServiceActions)
		}
	}
}

func TestWorkflowWithoutPostActionsUnaffected(t *testing.T) {
	state := &firewallTransactionalWorkflowState{reloadOK: true}
	response, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{
		Confirm: true, ApplyLive: true, ApplyServices: true,
	})
	if err != nil || status != http.StatusOK || !response.Applied {
		t.Fatalf("status=%d err=%v response=%+v", status, err, response)
	}
}
