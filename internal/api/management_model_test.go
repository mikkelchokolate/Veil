package api

import "testing"

func TestDefaultManagementModelStartsWithoutInitialInboundsAndKeepsRouting(t *testing.T) {
	model := defaultManagementModel(ServerInfo{Mode: "server"})
	if model.Settings.PanelListen != "127.0.0.1:2096" || model.Settings.Mode != "server" || model.Settings.Stack != "panel" {
		t.Fatalf("unexpected default settings: %+v", model.Settings)
	}
	if len(model.Inbounds) != 0 {
		t.Fatalf("unexpected default inbounds: %+v", model.Inbounds)
	}
	if len(model.Rules) != 1 || model.Rules[0].Name != "default-direct" {
		t.Fatalf("unexpected default routing rules: %+v", model.Rules)
	}
	if model.Warp.Endpoint != "engage.cloudflareclient.com:2408" {
		t.Fatalf("unexpected WARP defaults: %+v", model.Warp)
	}
}

func TestDefaultManagementModelCarriesPanelCaddyAccessDefaults(t *testing.T) {
	model := defaultManagementModel(ServerInfo{Mode: "server", PanelListen: "127.0.0.1:31096", PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "/panel-secret/"})
	if model.Settings.PanelListen != "127.0.0.1:31096" || model.Settings.PanelAccess != "caddy" || model.Settings.WebBasePath != "/panel-secret/" {
		t.Fatalf("Panel Caddy access defaults not carried into settings: %+v", model.Settings)
	}
	if model.Settings.Domain != "panel.example.com" || model.Settings.Email != "admin@example.com" {
		t.Fatalf("domain/email defaults not carried into settings: %+v", model.Settings)
	}
}
