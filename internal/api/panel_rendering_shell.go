package api

import "strings"

func renderPanelHTMLBase() string {
	html := strings.ReplaceAll(panelHTMLBase, panelIntroCardsPlaceholder, panelIntroCardsHTML())
	html = strings.ReplaceAll(html, panelInboundFormPlaceholder, panelInboundFormHTML())
	html = strings.ReplaceAll(html, panelApplyCardPlaceholder, panelApplyCardHTML())
	html = strings.ReplaceAll(html, panelClientLinksCardPlaceholder, panelClientLinksCardHTML())
	html = strings.ReplaceAll(html, panelSettingsCardPlaceholder, panelSettingsCardHTML())
	html = strings.ReplaceAll(html, panelWarpCardPlaceholder, panelWarpCardHTML())
	html = strings.ReplaceAll(html, panelRoutingCardPlaceholder, panelRoutingCardHTML())
	html = strings.ReplaceAll(html, panelServiceStatusCardPlaceholder, panelServiceStatusCardHTML())
	html = strings.ReplaceAll(html, panelRuntimeStatsCardsPlaceholder, panelRuntimeStatsCardsHTML())
	html = strings.ReplaceAll(html, panelDiagnosticsCardsPlaceholder, panelDiagnosticsCardsHTML())
	replacer := strings.NewReplacer(
		panelClientProfileControlsPlaceholder, panelClientProfileControlsHTML(),
		panelClientProfileActionsPlaceholder, panelClientProfileActionsJS(),
		panelClientLinksActionsPlaceholder, panelClientLinksActionsJS(),
		panelServiceStatusActionsPlaceholder, panelServiceStatusActionsJS(),
		panelApplyActionsPlaceholder, panelApplyActionsJS(),
		panelSettingsActionsPlaceholder, panelSettingsActionsJS(),
		panelInboundActionsPlaceholder, panelInboundActionsJS(),
		panelWarpActionsPlaceholder, panelWarpActionsJS(),
		panelRoutingActionsPlaceholder, panelRoutingActionsJS(),
		panelRuntimeStatsActionsPlaceholder, panelRuntimeStatsActionsJS(),
		panelDiagnosticsActionsPlaceholder, panelDiagnosticsActionsJS(),
		panelIntroActionsPlaceholder, panelIntroActionsJS(),
		panelServiceRestartActionsPlaceholder, panelServiceRestartActionsJS(),
		panelUtilityActionsPlaceholder, panelUtilityActionsJS(),
		panelEventBindingsPlaceholder, panelEventBindingsJS(),
	)
	return replacer.Replace(html)
}
