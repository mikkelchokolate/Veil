package api

type PanelSlice struct {
	Name          string
	RenderSlots   []PanelRenderSlot
	EventBindings []PanelEventBinding
}

type PanelSliceCatalog struct{}

func NewPanelSliceCatalog() PanelSliceCatalog { return PanelSliceCatalog{} }

func (PanelSliceCatalog) Slices() []PanelSlice {
	return []PanelSlice{
		{
			Name:        "intro",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelIntroCardsPlaceholder, Render: panelIntroCardsHTML}, {Placeholder: panelIntroActionsPlaceholder, Render: panelIntroActionsJS}},
		},
		{
			Name:        "service-status",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelServiceStatusCardPlaceholder, Render: panelServiceStatusCardHTML}, {Placeholder: panelServiceStatusActionsPlaceholder, Render: panelServiceStatusActionsJS}, {Placeholder: panelServiceRestartActionsPlaceholder, Render: panelServiceRestartActionsJS}},
			EventBindings: []PanelEventBinding{
				{ElementID: "load-service-status", Handler: "loadServiceStatus", Event: "click"},
			},
		},
		{
			Name:        "runtime-stats",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelRuntimeStatsCardsPlaceholder, Render: panelRuntimeStatsCardsHTML}, {Placeholder: panelRuntimeStatsActionsPlaceholder, Render: panelRuntimeStatsActionsJS}},
		},
		{
			Name:        "client-links",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelClientLinksCardPlaceholder, Render: panelClientLinksCardHTML}, {Placeholder: panelClientLinksActionsPlaceholder, Render: panelClientLinksActionsJS}},
			EventBindings: []PanelEventBinding{
				{ElementID: "load-client-links", Handler: "loadClientLinks", Event: "click"},
				{ElementID: "load-client-subscription", Handler: "loadClientSubscription", Event: "click"},
				{ElementID: "load-client-subscription-raw", Handler: "loadRawClientSubscription", Event: "click"},
				{ElementID: "download-mieru-configs", Handler: "downloadMieruConfigs", Event: "click"},
				{ElementID: "copy-client-links", Handler: "copyClientLinksOutput", Event: "click"},
			},
		},
		{
			Name:        "settings",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelSettingsCardPlaceholder, Render: panelSettingsCardHTML}, {Placeholder: panelSettingsActionsPlaceholder, Render: panelSettingsActionsJS}},
			EventBindings: []PanelEventBinding{
				{ElementID: "settings-form", Handler: "saveSettings", Event: "submit"},
				{ElementID: "load-settings", Handler: "loadSettingsIntoForm", Event: "click"},
			},
		},
		{
			Name: "inbounds",
			RenderSlots: []PanelRenderSlot{
				{Placeholder: panelInboundFormPlaceholder, Render: panelInboundFormHTML},
				{Placeholder: panelInboundActionsPlaceholder, Render: panelInboundActionsJS},
				{Placeholder: panelClientProfileControlsPlaceholder, Render: panelClientProfileControlsHTML},
				{Placeholder: panelClientProfileActionsPlaceholder, Render: panelClientProfileActionsJS},
			},
			EventBindings: []PanelEventBinding{
				{ElementID: "inbound-protocol", Handler: "syncInboundTransportOptions", Event: "change"},
				{ElementID: "inbound-form", Handler: "saveInbound", Event: "submit"},
				{ElementID: "delete-inbound", Handler: "deleteInbound", Event: "click"},
				{ElementID: "load-inbounds", Handler: "loadInboundsIntoOutput", Event: "click"},
			},
		},
		{
			Name:        "routing",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelRoutingCardPlaceholder, Render: panelRoutingCardHTML}, {Placeholder: panelRoutingActionsPlaceholder, Render: panelRoutingActionsJS}},
			EventBindings: []PanelEventBinding{
				{ElementID: "routing-rule-form", Handler: "saveRoutingRule", Event: "submit"},
				{ElementID: "delete-routing-rule", Handler: "deleteRoutingRule", Event: "click"},
				{ElementID: "apply-routing-preset", Handler: "applyRoutingPreset", Event: "click"},
			},
		},
		{
			Name:        "warp",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelWarpCardPlaceholder, Render: panelWarpCardHTML}, {Placeholder: panelWarpActionsPlaceholder, Render: panelWarpActionsJS}},
			EventBindings: []PanelEventBinding{
				{ElementID: "warp-form", Handler: "saveWarpConfig", Event: "submit"},
				{ElementID: "load-warp-config", Handler: "loadWarpIntoForm", Event: "click"},
			},
		},
		{
			Name:        "apply",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelApplyCardPlaceholder, Render: panelApplyCardHTML}, {Placeholder: panelApplyActionsPlaceholder, Render: panelApplyActionsJS}},
		},
		{
			Name:        "diagnostics",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelDiagnosticsCardsPlaceholder, Render: panelDiagnosticsCardsHTML}, {Placeholder: panelDiagnosticsActionsPlaceholder, Render: panelDiagnosticsActionsJS}},
		},
		{
			Name:        "utility",
			RenderSlots: []PanelRenderSlot{{Placeholder: panelUtilityActionsPlaceholder, Render: panelUtilityActionsJS}, {Placeholder: panelEventBindingsPlaceholder, Render: panelEventBindingsJS}},
		},
	}
}

func (c PanelSliceCatalog) Slice(name string) (PanelSlice, bool) {
	for _, slice := range c.Slices() {
		if slice.Name == name {
			return slice, true
		}
	}
	return PanelSlice{}, false
}

func (c PanelSliceCatalog) RenderSlots() []PanelRenderSlot {
	var slots []PanelRenderSlot
	for _, slice := range c.Slices() {
		slots = append(slots, slice.RenderSlots...)
	}
	return slots
}

func (c PanelSliceCatalog) EventBindings() []PanelEventBinding {
	var bindings []PanelEventBinding
	for _, slice := range c.Slices() {
		bindings = append(bindings, slice.EventBindings...)
	}
	return bindings
}
