package api

import "github.com/veil-panel/veil/internal/panel"

type PanelRenderSlot = panel.RenderSlot

type PanelRenderSlotCatalog struct{}

func NewPanelRenderSlotCatalog() PanelRenderSlotCatalog { return PanelRenderSlotCatalog{} }

func (PanelRenderSlotCatalog) Slots() []PanelRenderSlot {
	return NewPanelSliceCatalog().RenderSlots()
}
