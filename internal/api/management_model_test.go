package api

import "testing"

func TestDefaultManagementModelContainsInitialInboundsAndRouting(t *testing.T) {
	model := defaultManagementModel("server")
	if model.Settings.PanelListen != "127.0.0.1:2096" || model.Settings.Mode != "server" {
		t.Fatalf("unexpected default settings: %+v", model.Settings)
	}
	if len(model.Inbounds) != 2 || model.Inbounds[0].Protocol != "naiveproxy" || model.Inbounds[1].Protocol != "hysteria2" {
		t.Fatalf("unexpected default inbounds: %+v", model.Inbounds)
	}
	if len(model.Rules) != 1 || model.Rules[0].Name != "default-direct" {
		t.Fatalf("unexpected default routing rules: %+v", model.Rules)
	}
	if model.Warp.Endpoint != "engage.cloudflareclient.com:2408" {
		t.Fatalf("unexpected WARP defaults: %+v", model.Warp)
	}
}
