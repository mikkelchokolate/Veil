package applyflow

import (
	"net/http"
	"testing"

	"github.com/veil-panel/veil/internal/model"
)

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

type fakeState struct {
	plan  model.ApplyPlanResponse
	wrote bool
}

func (s *fakeState) BuildApplyPlanLocked() model.ApplyPlanResponse { return s.plan }
func (s *fakeState) WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error) {
	s.wrote = true
	return nil, nil, nil, nil
}
func (s *fakeState) PromoteStagedConfigsLocked([]string) ([]string, []string, []PromotionRecord, error) {
	return nil, nil, nil, nil
}
func (s *fakeState) ReloadPromotedServicesLocked([]string) []model.ServiceActionResult { return nil }
func (s *fakeState) RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []model.ServiceActionResult) {
	return nil, nil
}
func (s *fakeState) AppendApplyHistoryLocked(string, bool, model.ApplyResponse) error { return nil }
