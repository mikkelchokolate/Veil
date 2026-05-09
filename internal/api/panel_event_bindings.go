package api

import panelui "github.com/veil-panel/veil/internal/panel"

const panelEventBindingsPlaceholder = panelui.EventBindingsPlaceholder

func panelEventBindingsJS() string {
	return panelui.EventBindingsJS(NewPanelEventBindingCatalog().Bindings())
}
