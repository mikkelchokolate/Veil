package api

import (
	"strings"
	"testing"
)

func TestManagementApplyIntentBuildsPlanWithRenderValidation(t *testing.T) {
	intent := NewManagementApplyIntent(ManagementApplyIntentInput{
		ApplyRoot: "/apply",
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds:  []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
	})

	plan := intent.BuildPlan()
	if plan.Valid || !strings.Contains(strings.Join(plan.Errors, "\n"), "mieru user name and password are required") {
		t.Fatalf("expected Mieru render validation error, got %+v", plan)
	}
}

func TestManagementApplyIntentCanSkipRenderValidationForOfflineStateChecks(t *testing.T) {
	intent := NewManagementApplyIntent(ManagementApplyIntentInput{
		ApplyRoot:       "/apply",
		Settings:        Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds:        []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
		SkipRenderCheck: true,
	})

	plan := intent.BuildPlan()
	if !plan.Valid {
		t.Fatalf("offline intent should skip render validation, got %+v", plan)
	}
}
