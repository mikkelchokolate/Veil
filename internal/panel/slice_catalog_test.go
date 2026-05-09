package panel

import (
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestSliceCatalogContainsApplyDiagnosticsAndServiceSlots(t *testing.T) {
	catalog := NewSliceCatalog([]service.ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: "veil.service", ManualRestart: true}})
	for _, name := range []string{"service-status", "apply", "diagnostics", "inbounds"} {
		if _, ok := catalog.Slice(name); !ok {
			t.Fatalf("missing slice %q", name)
		}
	}
	slots := catalog.RenderSlots()
	for _, placeholder := range []string{ServiceStatusCardPlaceholder, ApplyCardPlaceholder, DiagnosticsCardsPlaceholder, panelInboundFormPlaceholder, panelInboundActionsPlaceholder, EventBindingsPlaceholder, ServiceRestartActionsPlaceholder} {
		if !hasSlot(slots, placeholder) {
			t.Fatalf("missing slot %q", placeholder)
		}
	}
	bindings := catalog.EventBindings()
	for id, handler := range map[string]string{
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
	} {
		if got := bindingHandler(bindings, id); got != handler {
			t.Fatalf("binding %q = %q, want %q; all=%+v", id, got, handler, bindings)
		}
	}
}

func hasSlot(slots []RenderSlot, placeholder string) bool {
	for _, slot := range slots {
		if slot.Placeholder == placeholder {
			return slot.Render != nil
		}
	}
	return false
}

func bindingHandler(bindings []EventBinding, elementID string) string {
	for _, binding := range bindings {
		if binding.ElementID == elementID {
			return binding.Handler
		}
	}
	return ""
}
