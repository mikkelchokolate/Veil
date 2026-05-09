package api

import panelui "github.com/veil-panel/veil/internal/panel"

const panelDiagnosticsCardsPlaceholder = panelui.DiagnosticsCardsPlaceholder

func panelDiagnosticsCardsHTML() string {
	return panelui.DiagnosticsCardsHTML(NewManagedRuntimeCatalog().Runtimes())
}

func panelManagedLogUnitOptionsHTML() string {
	return panelui.ManagedLogUnitOptionsHTML(NewManagedRuntimeCatalog().Runtimes())
}
