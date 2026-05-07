package api

import (
	"strings"
	"testing"
)

func TestBuildApplyPlanRequiresCaddySettingsForNaiveProxyInbound(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: "dev"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if plan.Valid {
		t.Fatalf("NaiveProxy without Caddy settings should be invalid: %+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Errors, "\n"), "domain, email, naive username, and naive password are required for NaiveProxy/Caddy") {
		t.Fatalf("missing Caddy settings error: %+v", plan.Errors)
	}
}

func TestBuildApplyPlanAcceptsNaiveProxyWithCaddySettings(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: "dev", Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("NaiveProxy with Caddy settings should be valid: %+v", plan)
	}
}
