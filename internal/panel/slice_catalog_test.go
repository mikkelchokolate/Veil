package panel

import (
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestSliceCatalogContainsApplyDiagnosticsAndServiceSlots(t *testing.T) {
	catalog := NewSliceCatalog([]service.ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: "veil.service", ManualRestart: true}})
	for _, name := range []string{"service-status", "apply", "diagnostics"} {
		if _, ok := catalog.Slice(name); !ok {
			t.Fatalf("missing slice %q", name)
		}
	}
	slots := catalog.RenderSlots()
	for _, placeholder := range []string{ServiceStatusCardPlaceholder, ApplyCardPlaceholder, DiagnosticsCardsPlaceholder} {
		if !hasSlot(slots, placeholder) {
			t.Fatalf("missing slot %q", placeholder)
		}
	}
	bindings := catalog.EventBindings()
	if len(bindings) == 0 || bindings[0].ElementID == "" {
		t.Fatalf("bindings = %+v", bindings)
	}
}

func hasSlot(slots []RenderSlot, placeholder string) bool {
	for _, slot := range slots {
		if slot.Placeholder == placeholder {
			return true
		}
	}
	return false
}
