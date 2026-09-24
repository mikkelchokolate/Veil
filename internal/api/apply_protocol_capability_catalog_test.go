package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestApplyProtocolCapabilityCatalogOwnsConfigActionsAndValidation(t *testing.T) {
	catalog := NewApplyProtocolCapabilityCatalog()
	// The zero-arg catalog anchors displayed paths at the configured etc
	// dir's generated tree — under the TestMain VEIL_LIVE_ROOT isolation that
	// is the isolated test root, not the packaged /etc/veil literal.
	genRoot := filepath.ToSlash(filepath.Join(hostenv.EtcDir(), "generated"))
	cases := []struct {
		protocol               string
		config                 string
		action                 string
		validateRender         bool
		requiresRenderSettings bool
		settingsError          bool
	}{
		{"naiveproxy", genRoot + "/caddy/config.json", "reload veil-caddy.service", true, false, false},
		// hysteria2/olcrtc render per-inbound artifacts (<dir>/<name>.yaml) and
		// per-inbound template instances (veil-*@<name>.service). With no
		// inbound selected there is no concrete config path or restartable
		// unit, so the catalog advertises neither a ghost server.yaml nor a
		// bare @.service restart (#780).
		{"hysteria2", "", "", true, true, false},
		{"mieru", genRoot + "/mieru/server_config.json", "restart veil-mieru.service", true, false, false},
		{"olcrtc", "", "", true, false, false},
	}
	for _, tc := range cases {
		capability, ok := catalog.ForProtocol(tc.protocol)
		if !ok {
			t.Fatalf("%s should be supported", tc.protocol)
		}
		if capability.Config != tc.config || capability.Action != tc.action || capability.ValidateInboundRender != tc.validateRender || capability.RequiresRenderSettings != tc.requiresRenderSettings {
			t.Fatalf("capability for %s = %+v", tc.protocol, capability)
		}
		err := capability.ValidateSettings(Settings{}, Inbound{})
		if tc.settingsError && err == nil {
			t.Fatalf("expected settings error for %s", tc.protocol)
		}
		if !tc.settingsError && err != nil {
			t.Fatalf("unexpected settings error for %s: %v", tc.protocol, err)
		}
	}
}

func TestApplyProtocolCapabilityCatalogRejectsUnknownProtocol(t *testing.T) {
	capability, ok := NewApplyProtocolCapabilityCatalog().ForProtocol("unknown")
	if ok || capability != (ApplyProtocolCapability{}) {
		t.Fatalf("unknown capability = %+v %v", capability, ok)
	}
}
