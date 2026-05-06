package api

import (
	"net/http"
	"testing"
)

func TestApplyWorkflowRejectsServicesWithoutLiveApply(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	workflow := NewApplyWorkflow(state)

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
