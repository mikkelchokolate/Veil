package api

type PanelRenderSlot struct {
	Placeholder string
	Render      func() string
}

type PanelRenderSlotCatalog struct{}

func NewPanelRenderSlotCatalog() PanelRenderSlotCatalog { return PanelRenderSlotCatalog{} }

func (PanelRenderSlotCatalog) Slots() []PanelRenderSlot {
	return []PanelRenderSlot{
		{Placeholder: panelIntroCardsPlaceholder, Render: panelIntroCardsHTML},
		{Placeholder: panelInboundFormPlaceholder, Render: panelInboundFormHTML},
		{Placeholder: panelApplyCardPlaceholder, Render: panelApplyCardHTML},
		{Placeholder: panelClientLinksCardPlaceholder, Render: panelClientLinksCardHTML},
		{Placeholder: panelSettingsCardPlaceholder, Render: panelSettingsCardHTML},
		{Placeholder: panelWarpCardPlaceholder, Render: panelWarpCardHTML},
		{Placeholder: panelRoutingCardPlaceholder, Render: panelRoutingCardHTML},
		{Placeholder: panelServiceStatusCardPlaceholder, Render: panelServiceStatusCardHTML},
		{Placeholder: panelRuntimeStatsCardsPlaceholder, Render: panelRuntimeStatsCardsHTML},
		{Placeholder: panelDiagnosticsCardsPlaceholder, Render: panelDiagnosticsCardsHTML},
		{Placeholder: panelClientProfileControlsPlaceholder, Render: panelClientProfileControlsHTML},
		{Placeholder: panelClientProfileActionsPlaceholder, Render: panelClientProfileActionsJS},
		{Placeholder: panelClientLinksActionsPlaceholder, Render: panelClientLinksActionsJS},
		{Placeholder: panelServiceStatusActionsPlaceholder, Render: panelServiceStatusActionsJS},
		{Placeholder: panelApplyActionsPlaceholder, Render: panelApplyActionsJS},
		{Placeholder: panelSettingsActionsPlaceholder, Render: panelSettingsActionsJS},
		{Placeholder: panelInboundActionsPlaceholder, Render: panelInboundActionsJS},
		{Placeholder: panelWarpActionsPlaceholder, Render: panelWarpActionsJS},
		{Placeholder: panelRoutingActionsPlaceholder, Render: panelRoutingActionsJS},
		{Placeholder: panelRuntimeStatsActionsPlaceholder, Render: panelRuntimeStatsActionsJS},
		{Placeholder: panelDiagnosticsActionsPlaceholder, Render: panelDiagnosticsActionsJS},
		{Placeholder: panelIntroActionsPlaceholder, Render: panelIntroActionsJS},
		{Placeholder: panelServiceRestartActionsPlaceholder, Render: panelServiceRestartActionsJS},
		{Placeholder: panelUtilityActionsPlaceholder, Render: panelUtilityActionsJS},
		{Placeholder: panelEventBindingsPlaceholder, Render: panelEventBindingsJS},
	}
}
