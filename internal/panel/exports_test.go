package panel

import (
	"strings"
	"testing"
)

// TestPublicExportFunctionsCallInternalRenderers verifies that every public
// HTML/JS export wrapper invokes its internal panel implementation and returns
// a non-empty result. These wrappers are thin, so the test focuses on ensuring
// they remain wired correctly and do not panic.
func TestPublicExportFunctionsCallInternalRenderers(t *testing.T) {
	cases := []struct {
		name string
		fn   func() string
		want string
	}{
		{"ClientLinksCardHTML", ClientLinksCardHTML, "client-links"},
		{"ClientLinksActionsJS", ClientLinksActionsJS, "clientLinks"},
		{"ClientProfileControlsHTML", ClientProfileControlsHTML, "client-profile"},
		{"ClientProfileActionsJS", ClientProfileActionsJS, "clientProfile"},
		{"InboundActionsJS", InboundActionsJS, "inbound"},
		{"DynamicFieldsJS", DynamicFieldsJS, "protocolField"},
		{"InboundProtocolOptionsHTML", InboundProtocolOptionsHTML, "naiveproxy"},
		{"InboundTransportOptionsHTML", InboundTransportOptionsHTML, "tcp"},
		{"InboundProtocolTransportRulesJS", InboundProtocolTransportRulesJS, "syncInboundTransportOptions"},
		{"IntroCardsHTML", IntroCardsHTML, "Dashboard"},
		{"IntroActionsJS", IntroActionsJS, "load-version"},
		{"RoutingCardHTML", RoutingCardHTML, "Routing"},
		{"RoutingActionsJS", RoutingActionsJS, "routing"},
		{"RuntimeStatsCardsHTML", RuntimeStatsCardsHTML, "System resources"},
		{"RuntimeStatsActionsJS", RuntimeStatsActionsJS, "load-system-stats"},
		{"SettingsCardHTML", SettingsCardHTML, "Settings"},
		{"SettingsActionsJS", SettingsActionsJS, "settings"},
		{"WarpCardHTML", WarpCardHTML, "WARP"},
		{"WarpActionsJS", WarpActionsJS, "warp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn()
			if strings.TrimSpace(got) == "" {
				t.Fatalf("%s returned empty output", tc.name)
			}
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.want)) {
				t.Errorf("%s output did not contain %q", tc.name, tc.want)
			}
		})
	}
}
