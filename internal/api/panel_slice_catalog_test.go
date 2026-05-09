package api

import "testing"

func TestPanelSliceCatalogKeepsInboundRenderingAndBindingsTogether(t *testing.T) {
	slice, ok := NewPanelSliceCatalog().Slice("inbounds")
	if !ok {
		t.Fatal("missing Inbounds Panel slice")
	}
	if !panelSliceHasRenderSlot(slice, panelInboundFormPlaceholder) || !panelSliceHasRenderSlot(slice, panelInboundActionsPlaceholder) {
		t.Fatalf("Inbounds Panel slice should own form and action slots: %+v", slice.RenderSlots)
	}
	for _, elementID := range []string{"inbound-protocol", "inbound-form", "delete-inbound", "load-inbounds"} {
		if !panelSliceHasEventBinding(slice, elementID) {
			t.Fatalf("Inbounds Panel slice missing binding for %s: %+v", elementID, slice.EventBindings)
		}
	}
}

func panelSliceHasRenderSlot(slice PanelSlice, placeholder string) bool {
	for _, slot := range slice.RenderSlots {
		if slot.Placeholder == placeholder {
			return true
		}
	}
	return false
}

func panelSliceHasEventBinding(slice PanelSlice, elementID string) bool {
	for _, binding := range slice.EventBindings {
		if binding.ElementID == elementID {
			return true
		}
	}
	return false
}
