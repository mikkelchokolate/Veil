package api

import panelui "github.com/veil-panel/veil/internal/panel"

const panelServiceStatusCardPlaceholder = panelui.ServiceStatusCardPlaceholder

func panelServiceStatusCardHTML() string {
	return panelui.ServiceStatusCardHTML(NewManagedRuntimeCatalog().Runtimes())
}
