package api

import "testing"

func TestManagementApplyContextBuildsApplyPlanFromState(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	ctx := NewManagementApplyContext(state)

	plan := ctx.buildApplyPlanLocked()
	if len(plan.Actions) == 0 {
		t.Fatalf("expected apply plan actions, got %+v", plan)
	}
}
