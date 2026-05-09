package api

import "strings"

type PanelEventBinding struct {
	ElementID string
	Handler   string
	Event     string
}

type PanelEventBindingCatalog struct{}

func NewPanelEventBindingCatalog() PanelEventBindingCatalog { return PanelEventBindingCatalog{} }

func (PanelEventBindingCatalog) Bindings() []PanelEventBinding {
	return NewPanelSliceCatalog().EventBindings()
}

func panelEventBindingCatalogJS() string {
	var b strings.Builder
	for _, binding := range NewPanelEventBindingCatalog().Bindings() {
		b.WriteString("    document.getElementById('")
		b.WriteString(binding.ElementID)
		b.WriteString("').addEventListener('")
		b.WriteString(binding.Event)
		b.WriteString("', ")
		b.WriteString(binding.Handler)
		b.WriteString(");\n")
	}
	return b.String()
}
