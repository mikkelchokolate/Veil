package api

import (
	"strings"
	"testing"
)

func TestBuildApplyPlanIncludesProtocolInboundValidationIssues(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{
			PanelListen:      "127.0.0.1:2096",
			Mode:             "dev",
			Domain:           "vpn.example.com",
			DefaultAcmeEmail: "admin@example.com",
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
	// The consolidated Caddy validation reports the missing credential as a plan
	// error when the inbound/Caddy redesign validation runs before the legacy
	// per-inbound validator.
	for _, err := range plan.Errors {
		if strings.Contains(err, "naive inbound \"naive\" is missing valid credentials") {
			return
		}
	}
	t.Fatalf("plan is missing protocol-specific inbound validation issue: %+v", plan)
}

func TestBuildApplyPlanDefersProtocolInboundIssuesUntilRequiredSettingsExist(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{
			PanelListen: "127.0.0.1:2096",
			Mode:        "dev",
			Domain:      "vpn.example.com",
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
		t.Fatalf("plan without required NaiveProxy settings should be invalid: %+v", plan)
	}
	if len(plan.Issues) != 0 {
		t.Fatalf("dependent inbound issues should be deferred until required settings exist: %+v", plan.Issues)
	}
}
