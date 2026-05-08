package api

import "testing"

func TestBuildApplyPlanRejectsRoutingRuleUsingDisabledWarp(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{Stack: "both"},
		Rules:    []RoutingRule{{Name: "non-ru", Match: "geosite:geolocation-!ru", Outbound: "warp", Enabled: true}},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "routing rule non-ru requires WARP to be enabled") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

func TestBuildApplyPlanUsesRenderValidationCallbacks(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings:                Settings{Stack: "both"},
		RenderSettingsAvailable: true,
		Inbounds:                []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
		ValidateInboundRender: func(inbound Inbound) error {
			return errApplyPlanTest("render failed")
		},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "render failed") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

func TestBuildApplyPlanUsesMieruRenderValidationWithoutRenderSettings(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{Stack: "mieru"},
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
		ValidateInboundRender: func(inbound Inbound) error {
			if inbound.Protocol == "mieru" {
				return errApplyPlanTest("mieru render failed")
			}
			return nil
		},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "mieru render failed") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

type errApplyPlanTest string

func (e errApplyPlanTest) Error() string { return string(e) }

func containsApplyPlanString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
