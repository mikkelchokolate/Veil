package api

import "testing"

func TestPanelEventBindingCatalogOwnsButtonToHandlerCoupling(t *testing.T) {
	bindings := NewPanelEventBindingCatalog().Bindings()
	want := map[string]string{
		"settings-form":          "saveSettings",
		"load-settings":          "loadSettingsIntoForm",
		"load-client-links":      "loadClientLinks",
		"inbound-form":           "saveInbound",
		"routing-rule-form":      "saveRoutingRule",
		"warp-form":              "saveWarpConfig",
		"load-warp-config":       "loadWarpIntoForm",
		"download-mieru-configs": "downloadMieruConfigs",
		"load-service-status":    "loadServiceStatus",
		"inbound-protocol":       "syncInboundTransportOptions",
	}
	seen := map[string]string{}
	for _, binding := range bindings {
		seen[binding.ElementID] = binding.Handler
	}
	for id, handler := range want {
		if seen[id] != handler {
			t.Fatalf("binding %q = %q, want %q; all=%+v", id, seen[id], handler, bindings)
		}
	}
}
