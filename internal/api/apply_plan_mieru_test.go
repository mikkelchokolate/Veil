package api

import "testing"

func TestBuildApplyPlanIncludesMieruConfigAndReloadAction(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("plan should be valid: %+v", plan)
	}
	if !containsString(plan.Configs, "/etc/veil/generated/mieru/server_config.json") {
		t.Fatalf("plan missing Mieru config: %+v", plan.Configs)
	}
	if !containsString(plan.Actions, "restart veil-mieru.service") {
		t.Fatalf("plan missing Mieru restart action: %+v", plan.Actions)
	}
	if containsString(plan.Configs, "/etc/veil/generated/caddy/Caddyfile") || containsString(plan.Actions, "reload veil-naive.service") {
		t.Fatalf("Mieru-only plan should not require Caddy/Naive actions: %+v", plan)
	}
}
