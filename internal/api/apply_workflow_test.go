package api

import (
	"net/http"
	"testing"
)

type fakeApplyWorkflowState struct {
	plan      ApplyPlanResponse
	written   bool
	promoted  bool
	histories []string
}

func (f *fakeApplyWorkflowState) buildApplyPlanLocked() ApplyPlanResponse { return f.plan }

func (f *fakeApplyWorkflowState) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	f.written = true
	return []string{"staged"}, []ConfigValidationResult{{Name: "caddy", Valid: true}}, []string{"rendered"}, nil
}

func (f *fakeApplyWorkflowState) promoteStagedConfigs(paths []string) ([]string, []string, []livePromotionRecord, error) {
	f.promoted = true
	return []string{"live"}, nil, []livePromotionRecord{{LivePath: "live"}}, nil
}

func (f *fakeApplyWorkflowState) reloadPromotedServices(files []string) []ServiceActionResult {
	return nil
}

func (f *fakeApplyWorkflowState) rollbackPromotedConfigs(records []livePromotionRecord, files []string) ([]string, []ServiceActionResult) {
	return nil, nil
}

func (f *fakeApplyWorkflowState) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	f.histories = append(f.histories, stage)
	return nil
}

func TestApplyWorkflowRunsAgainstStateAdapter(t *testing.T) {
	state := &fakeApplyWorkflowState{plan: ApplyPlanResponse{Valid: true}}
	workflow := NewApplyWorkflow(state)

	response, status, err := workflow.RunLocked(ApplyRequest{Confirm: true, ApplyLive: true})
	if err != nil {
		t.Fatalf("RunLocked: %v", err)
	}
	if status != http.StatusOK || response.Applied || !response.LiveApplied {
		t.Fatalf("unexpected response/status: status=%d response=%+v", status, response)
	}
	if !state.written || !state.promoted {
		t.Fatalf("workflow did not use state Adapter: %+v", state)
	}
	if len(state.histories) != 1 || state.histories[0] != "live" {
		t.Fatalf("history stages = %+v", state.histories)
	}
}

func TestApplyWorkflowRejectsServicesWithoutLiveApply(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	workflow := NewApplyWorkflow(NewManagementApplyContext(state))

	response, status, err := workflow.RunLocked(ApplyRequest{Confirm: true, ApplyServices: true, ApplyLive: false})
	if err != nil {
		t.Fatalf("unexpected text error: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
	}
	if response.Applied {
		t.Fatalf("response should not be applied: %+v", response)
	}
}
