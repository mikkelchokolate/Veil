package api

type PanelRenderSlot struct {
	Placeholder string
	Render      func() string
}

type PanelRenderSlotCatalog struct{}

func NewPanelRenderSlotCatalog() PanelRenderSlotCatalog { return PanelRenderSlotCatalog{} }

func (PanelRenderSlotCatalog) Slots() []PanelRenderSlot {
	return NewPanelSliceCatalog().RenderSlots()
}
