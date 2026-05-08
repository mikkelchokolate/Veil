package api

import "testing"

func TestBuildApplyPlanReportsRuntimeUnitsRequiredByEnabledInbounds(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Stack: "panel", Mode: "server"},
		Inbounds: []Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"},
			{Name: "disabled-naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: false},
		},
	})
	if !equalStrings(plan.Runtimes, []string{"veil-mieru.service"}) {
		t.Fatalf("plan runtimes = %+v", plan.Runtimes)
	}
}
