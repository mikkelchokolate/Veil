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
		t.Fatalf("Panel Caddy apply plan should require domain/email: %+v", plan)
	}
}

func TestBuildApplyPlanRejectsPanelCaddyTCP443RuntimeConflict(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server", PanelDomain: "panel.example.com", PanelEmail: "admin@example.com"},
		Inbounds: []Inbound{{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if plan.Valid || !strings.Contains(strings.Join(plan.Errors, "\n"), ":443 is claimed by multiple owners") {
		t.Fatalf("Panel Caddy should reject non-Caddy TCP 443 inbound conflict: %+v", plan)
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
	if !strings.Contains(strings.Join(plan.Errors, "\n"), "naive inbound \"naive\" is missing a public domain") {
		t.Fatalf("missing Caddy settings error: %+v", plan.Errors)
	}
}

func TestBuildApplyPlanAcceptsNaiveProxyWithCaddySettings(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("NaiveProxy with Caddy settings should be valid: %+v", plan)
	}
}

func TestBuildApplyPlanAcceptsNaiveProxyWithInboundCredentials(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, NaiveUsername: "veil", NaivePassword: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("NaiveProxy with inbound credentials should be valid: %+v", plan)
	}
}

func TestBuildApplyPlanRejectsNaiveProxyWithMissingInboundCredentials(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})
	if plan.Valid {
		t.Fatalf("NaiveProxy without credentials should be invalid: %+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Errors, "\n"), "naive inbound \"naive\" is missing valid credentials") {
		t.Fatalf("missing inbound credential error: %+v", plan.Errors)
	}
}
