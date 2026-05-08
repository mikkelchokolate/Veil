package api

import "testing"

func TestBuildApplyPlanAcceptsMieruAndPanelStacks(t *testing.T) {
	mieru := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !mieru.Valid || !containsString(mieru.Configs, "/etc/veil/generated/mieru/server_config.json") {
		t.Fatalf("mieru stack plan = %+v", mieru)
	}
	panel := BuildApplyPlan(ApplyPlanInput{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}})
	if !panel.Valid || len(panel.Configs) != 0 {
		t.Fatalf("panel stack plan = %+v", panel)
	}
}
