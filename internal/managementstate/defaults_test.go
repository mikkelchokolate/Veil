package managementstate

import "testing"

func TestDefaultModelCarriesPanelCaddyAccessDefaults(t *testing.T) {
	model := BuildDefaultState(DefaultInput{Mode: "server", PanelListen: "127.0.0.1:31096", PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "/panel-secret/"})
	if model.Settings.PanelListen != "127.0.0.1:31096" || model.Settings.PanelAccess != "caddy" || model.Settings.WebBasePath != "/panel-secret/" || model.Settings.Mode != "server" {
		t.Fatalf("settings = %+v", model.Settings)
	}
	if len(model.Inbounds) != 0 {
		t.Fatalf("inbounds = %+v", model.Inbounds)
	}
	if model.Warp.Endpoint != "engage.cloudflareclient.com:2408" || len(model.Rules) != 1 || model.Rules[0].Name != "default-direct" {
		t.Fatalf("defaults = %+v", model)
	}
}

func TestDefaultModelInfersCaddyAccessFromWebBasePath(t *testing.T) {
	state := BuildDefaultState(DefaultInput{WebBasePath: "/panel-secret/"})
	if state.Settings.PanelAccess != "caddy" {
		t.Fatalf("PanelAccess = %q", state.Settings.PanelAccess)
	}
}

func TestDefaultModelFillsAllDefaults(t *testing.T) {
	state := BuildDefaultState(DefaultInput{})
	if state.Settings.PanelListen != "127.0.0.1:2096" {
		t.Fatalf("PanelListen = %q", state.Settings.PanelListen)
	}
	if state.Settings.Mode != "server" {
		t.Fatalf("Mode = %q", state.Settings.Mode)
	}
	if state.Settings.PanelAccess != "" {
		t.Fatalf("PanelAccess = %q", state.Settings.PanelAccess)
	}
	if state.Settings.WebBasePath != "" {
		t.Fatalf("WebBasePath = %q", state.Settings.WebBasePath)
	}
	if state.Settings.FirewallManagement == nil || !*state.Settings.FirewallManagement {
		t.Fatal("expected firewall management enabled by default")
	}
	if len(state.Inbounds) != 0 || len(state.Rules) != 1 {
		t.Fatalf("unexpected slices: inbounds=%+v rules=%+v", state.Inbounds, state.Rules)
	}
}

func TestDefaultModelCleansRootWebBasePath(t *testing.T) {
	state := BuildDefaultState(DefaultInput{WebBasePath: "/"})
	if state.Settings.WebBasePath != "" {
		t.Fatalf("WebBasePath = %q", state.Settings.WebBasePath)
	}
	if state.Settings.PanelAccess != "" {
		t.Fatalf("PanelAccess = %q", state.Settings.PanelAccess)
	}
}

func TestDefaultModelDoesNotInferCaddyWithoutWebBasePath(t *testing.T) {
	state := BuildDefaultState(DefaultInput{PanelAccess: "direct"})
	if state.Settings.PanelAccess != "direct" {
		t.Fatalf("PanelAccess = %q", state.Settings.PanelAccess)
	}
}
