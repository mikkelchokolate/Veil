package api

import panelui "github.com/veil-panel/veil/internal/panel"

type PanelSlice = panelui.Slice

type PanelSliceCatalog struct {
	inner panelui.SliceCatalog
}

func NewPanelSliceCatalog() PanelSliceCatalog {
	return PanelSliceCatalog{inner: panelui.NewSliceCatalog(NewManagedRuntimeCatalog().Runtimes())}
}

func (c PanelSliceCatalog) Slices() []PanelSlice {
	return c.inner.Slices()
}

func (c PanelSliceCatalog) Slice(name string) (PanelSlice, bool) {
	return c.inner.Slice(name)
}

func (c PanelSliceCatalog) RenderSlots() []PanelRenderSlot {
	return c.inner.RenderSlots()
}

func (c PanelSliceCatalog) EventBindings() []PanelEventBinding {
	return c.inner.EventBindings()
}
