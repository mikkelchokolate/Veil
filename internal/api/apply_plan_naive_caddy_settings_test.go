package api

import (
	"strings"
	"testing"
)

func TestBuildApplyPlanRejectsPanelCaddyAccessWithoutDomainEmail(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server"},
	})
	if plan.Valid || !strings.Contains(strings.Join(plan.Errors, "\n"), "panelDomain and panelEmail are required for caddy Panel access") {
		t.Fatalf("Panel Caddy apply plan should require panelDomain/panelEmail: %+v", plan)
	}
}

func TestBuildApplyPlanRejectsPanelCaddyTCP443RuntimeConflict(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server", PanelDomain: "panel.example.com", PanelEmail: "admin@example.com"},
		Inbounds: []Inbound{{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if plan.Valid {
		t.Fatalf("Panel Caddy should reject non-Caddy TCP 443 inbound conflict: %+v", plan)
	}
	found := false
	for _, err := range plan.Errors {
		if strings.Contains(err, "is claimed by multiple owners") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected bind conflict error, got: %+v", plan.Errors)
	}
}

func TestBuildApplyPlanIncludesPanelCaddyAccessWithoutNaiveInbound(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server", PanelDomain: "panel.example.com", PanelEmail: "admin@example.com"},
	})
	if !plan.Valid {
		t.Fatalf("Panel Caddy access plan should be valid: %+v", plan)
	}
	if !containsString(plan.Configs, "/etc/veil/generated/caddy/config.json") || !containsString(plan.Actions, "reload veil-caddy.service") || !containsString(plan.Runtimes, "veil-caddy.service") {
		t.Fatalf("Panel Caddy access plan missing config/action/runtime: %+v", plan)
	}
}

func TestBuildApplyPlanRequiresCaddySettingsForNaiveProxyInbound(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
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
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("NaiveProxy with Caddy settings should be valid: %+v", plan)
	}
}
