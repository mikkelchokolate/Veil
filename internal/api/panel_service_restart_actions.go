package api

import panelui "github.com/veil-panel/veil/internal/panel"

const panelServiceRestartActionsPlaceholder = panelui.ServiceRestartActionsPlaceholder

func panelServiceRestartActionsJS() string {
	return panelui.ServiceRestartActionsJS(NewManagedRuntimeCatalog().Runtimes())
}
