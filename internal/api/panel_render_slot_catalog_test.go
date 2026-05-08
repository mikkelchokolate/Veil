package api

import "testing"

func TestPanelRenderSlotCatalogOwnsPanelPlaceholderComposition(t *testing.T) {
	slots := NewPanelRenderSlotCatalog().Slots()
	want := map[string]bool{
		panelIntroCardsPlaceholder:            false,
		panelServiceStatusCardPlaceholder:     false,
		panelClientLinksCardPlaceholder:       false,
		panelSettingsActionsPlaceholder:       false,
		panelInboundActionsPlaceholder:        false,
		panelEventBindingsPlaceholder:         false,
		panelServiceRestartActionsPlaceholder: false,
	}
	for _, slot := range slots {
		if _, ok := want[slot.Placeholder]; ok {
			want[slot.Placeholder] = true
		}
		if slot.Render == nil {
			t.Fatalf("slot %q has nil render", slot.Placeholder)
		}
	}
	for placeholder, found := range want {
		if !found {
			t.Fatalf("render slot catalog missing %q", placeholder)
		}
	}
}
