package api

import "testing"

func TestBuildApplyPlanIncludesProtocolInboundValidationIssues(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{
			PanelListen: "127.0.0.1:2096",
			Mode:        "dev",
			Domain:      "vpn.example.com",
			Email:       "admin@example.com",
		},
		Inbounds: []Inbound{{
			Name:      "naive",
			Protocol:  "naiveproxy",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
		}},
	})

	if plan.Valid {
		t.Fatalf("plan without NaiveProxy credentials should be invalid: %+v", plan)
	}
	for _, issue := range plan.Issues {
		if issue.Code == "naive_credential_required" && issue.InboundID == "naive" {
			return
		}
	}
	t.Fatalf("plan is missing protocol-specific inbound validation issue: %+v", plan.Issues)
}
