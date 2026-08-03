package applyflow

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// fakeState is a configurable State double covering every RunLocked branch.
type fakeState struct {
	plan model.ApplyPlanResponse

	wrote       bool
	written     []string
	validations []model.ConfigValidationResult
	rendered    []string
	writeErr    error

	liveFiles   []string
	backupFiles []string
	promotions  []PromotionRecord
	promoteErr  error

	serviceActions []model.ServiceActionResult

	rollbackFiles   []string
	rollbackActions []model.ServiceActionResult
	rolledBack      bool

	history []string
}

func (s *fakeState) BuildApplyPlanLocked() model.ApplyPlanResponse { return s.plan }
func (s *fakeState) WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error) {
	s.wrote = true
	return s.written, s.validations, s.rendered, s.writeErr
}
func (s *fakeState) PromoteStagedConfigsLocked([]string) ([]string, []string, []PromotionRecord, error) {
	return s.liveFiles, s.backupFiles, s.promotions, s.promoteErr
}
func (s *fakeState) ReloadPromotedServicesLocked([]string) []model.ServiceActionResult {
	return s.serviceActions
}
func (s *fakeState) RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []model.ServiceActionResult) {
	s.rolledBack = true
	return s.rollbackFiles, s.rollbackActions
}
func (s *fakeState) AppendApplyHistoryLocked(stage string, _ bool, _ model.ApplyResponse) error {
	s.history = append(s.history, stage)
	return nil
}

func healthAllHealthy(actions []model.ServiceActionResult) []model.ServiceHealthResult {
	out := make([]model.ServiceHealthResult, 0, len(actions))
	for _, a := range actions {
		out = append(out, model.ServiceHealthResult{Name: a.Name, Healthy: true})
	}
	return out
}

func TestWorkflowRequiresConfirmBeforeWritingStage(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}}
	response, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{})
	if err == nil || status != http.StatusBadRequest || response.Applied {
		t.Fatalf("response=%+v status=%d err=%v", response, status, err)
	}
	if state.wrote {
		t.Fatalf("workflow wrote staged files without confirm")
	}
}

func TestWorkflowRejectsInvalidPlan(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: false}}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if status != http.StatusBadRequest || err != nil || resp.Applied {
		t.Fatalf("invalid plan must 400 without applying: resp=%+v status=%d err=%v", resp, status, err)
	}
	if state.wrote {
		t.Fatal("invalid plan must not write stage")
	}
}

func TestWorkflowRejectsServicesWithoutLive(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}}
	resp, status, _ := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyServices: true})
	if status != http.StatusBadRequest || resp.Applied {
		t.Fatalf("applyServices without applyLive must 400: resp=%+v status=%d", resp, status)
	}
}

func TestWorkflowStagesOnlyWithConfirm(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, written: []string{"a.json"}}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("stage-only should succeed: status=%d err=%v", status, err)
	}
	if resp.Applied || resp.LiveApplied || resp.ServicesApplied {
		t.Fatalf("stage-only flags wrong: %+v", resp)
	}
	if len(state.history) != 1 || state.history[0] != "staged" {
		t.Fatalf("history = %v, want [staged]", state.history)
	}
}

func TestWorkflowSkippedValidationDoesNotBlockLiveApply(t *testing.T) {
	state := &fakeState{
		plan:        model.ApplyPlanResponse{Valid: true},
		validations: []model.ConfigValidationResult{{Name: "mieru", Skipped: true}},
		liveFiles:   []string{"/live/mieru/server_config.json"},
	}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("skipped validation must not block live apply: status=%d err=%v", status, err)
	}
	if !resp.LiveApplied {
		t.Fatalf("expected live applied, got %+v", resp)
	}
}

func TestWorkflowBlocksLiveApplyOnFailedValidation(t *testing.T) {
	state := &fakeState{
		plan:        model.ApplyPlanResponse{Valid: true},
		validations: []model.ConfigValidationResult{{Name: "caddy", Valid: false, Error: "bad config"}},
	}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if status != http.StatusBadRequest || err != nil {
		t.Fatalf("failed validation must 400: status=%d err=%v", status, err)
	}
	if resp.LiveApplied {
		t.Fatal("must not promote when validation fails")
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "validation" {
		t.Fatalf("history = %v, want last 'validation'", state.history)
	}
}

func TestWorkflowAppliesServicesAndPassesHealth(t *testing.T) {
	state := &fakeState{
		plan:           model.ApplyPlanResponse{Valid: true},
		validations:    []model.ConfigValidationResult{{Name: "mieru", Valid: true}},
		liveFiles:      []string{"/live/mieru/server_config.json"},
		serviceActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("full apply should succeed: status=%d err=%v", status, err)
	}
	if !resp.Applied || !resp.LiveApplied || !resp.ServicesApplied || resp.RolledBack {
		t.Fatalf("full apply flags wrong: %+v", resp)
	}
	if len(resp.HealthChecks) != 1 || !resp.HealthChecks[0].Healthy {
		t.Fatalf("health checks = %+v", resp.HealthChecks)
	}
	if state.history[len(state.history)-1] != "services" {
		t.Fatalf("history = %v, want last 'services'", state.history)
	}
}

func TestWorkflowRollsBackOnServiceActionFailure(t *testing.T) {
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "start failed"}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, _ := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("service failure must 400, got %d", status)
	}
	if !resp.RolledBack || !state.rolledBack {
		t.Fatalf("expected rollback, got %+v", resp)
	}
	if resp.ServicesApplied {
		t.Fatal("ServicesApplied must be false on rollback")
	}
}

func TestWorkflowRollsBackOnHealthFailure(t *testing.T) {
	unhealthy := func([]model.ServiceActionResult) []model.ServiceHealthResult {
		return []model.ServiceHealthResult{{Name: "veil-mieru.service", Healthy: false, Error: "down"}}
	}
	state := &fakeState{
		plan:           model.ApplyPlanResponse{Valid: true},
		liveFiles:      []string{"/live/x"},
		serviceActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
		rollbackFiles:  []string{"/live/x"},
	}
	resp, status, _ := NewWorkflow(state, unhealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("health failure must 400, got %d", status)
	}
	if resp.RolledBack || !resp.Ambiguous {
		t.Fatalf("unhealthy rollback must remain recovery-pending, got %+v", resp)
	}
}

func TestWorkflowLeavesRecoveryPendingWhenRestoredServiceActionFails(t *testing.T) {
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "reload failed"}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "restart previous generation failed"}},
	}
	resp, status, _ := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d response=%+v", status, resp)
	}
	if resp.RolledBack || resp.RollbackComplete || !resp.Ambiguous {
		t.Fatalf("failed service restoration was reported complete: %+v", resp)
	}
}

func TestWorkflowLeavesRecoveryPendingWhenRestoredServiceUnhealthy(t *testing.T) {
	checks := 0
	health := func([]model.ServiceActionResult) []model.ServiceHealthResult {
		checks++
		return []model.ServiceHealthResult{{Name: "veil-mieru.service", Healthy: false, Error: "restored generation unhealthy"}}
	}
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, _ := NewWorkflow(state, health).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest || checks != 2 {
		t.Fatalf("status=%d checks=%d response=%+v", status, checks, resp)
	}
	if resp.RolledBack || resp.PostRollbackHealthPass || resp.RollbackComplete || !resp.Ambiguous {
		t.Fatalf("unhealthy restored service was reported complete: %+v", resp)
	}
}

func TestWorkflowReturns500OnStageWriteError(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, writeErr: errors.New("disk full")}
	_, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("stage write error must 500: status=%d err=%v", status, err)
	}
}

func TestWorkflowReturns500OnPromoteError(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, promoteErr: errors.New("helper down")}
	_, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("promote error must 500: status=%d err=%v", status, err)
	}
}
