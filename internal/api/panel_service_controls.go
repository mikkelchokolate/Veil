package api

import panelui "github.com/veil-panel/veil/internal/panel"

func panelServiceRestartControlsHTML() string {
	return panelui.ServiceRestartControlsHTML(NewManagedRuntimeCatalog().Runtimes())
}

func panelServiceRestartControlActionsJS() string {
	return panelui.ServiceRestartActionsJS(NewManagedRuntimeCatalog().Runtimes())
}
