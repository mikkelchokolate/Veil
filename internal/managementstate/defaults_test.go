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
	model := BuildDefaultState(DefaultInput{WebBasePath: "/panel-secret/"})
	if model.Settings.PanelAccess != "caddy" {
		t.Fatalf("PanelAccess = %q", model.Settings.PanelAccess)
	}
}
