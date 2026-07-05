package api

import "testing"

func TestApplyProtocolCapabilityCatalogOwnsConfigActionsAndValidation(t *testing.T) {
	catalog := NewApplyProtocolCapabilityCatalog()
	cases := []struct {
		protocol               string
		config                 string
		action                 string
		validateRender         bool
		requiresRenderSettings bool
	}{
		{"naiveproxy", "", "", true, true},
		{"hysteria2", "/etc/veil/generated/hysteria2/server.yaml", "restart veil-hysteria2@.service", true, true},
		{"mieru", "/etc/veil/generated/mieru/server_config.json", "restart veil-mieru.service", true, false},
	}
	for _, tc := range cases {
		capability, ok := catalog.ForProtocol(tc.protocol)
		if !ok {
			t.Fatalf("%s should be supported", tc.protocol)
		}
		if capability.Config != tc.config || capability.Action != tc.action || capability.ValidateInboundRender != tc.validateRender || capability.RequiresRenderSettings != tc.requiresRenderSettings {
			t.Fatalf("capability for %s = %+v", tc.protocol, capability)
		}
	}
}

func TestApplyProtocolCapabilityCatalogRejectsUnknownProtocol(t *testing.T) {
	capability, ok := NewApplyProtocolCapabilityCatalog().ForProtocol("unknown")
	if ok || capability != (ApplyProtocolCapability{}) {
		t.Fatalf("unknown capability = %+v %v", capability, ok)
	}
}
