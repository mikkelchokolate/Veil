package api

import "github.com/veil-panel/veil/internal/panel"

type PanelEventBinding = panel.EventBinding

type PanelEventBindingCatalog struct{}

func NewPanelEventBindingCatalog() PanelEventBindingCatalog { return PanelEventBindingCatalog{} }

func (PanelEventBindingCatalog) Bindings() []PanelEventBinding {
	return NewPanelSliceCatalog().EventBindings()
}

func panelEventBindingCatalogJS() string {
	return panel.RenderEventBindings(NewPanelEventBindingCatalog().Bindings())
}
