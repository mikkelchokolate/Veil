package panel

import "github.com/mikkelchokolate/Veil/internal/service"

const EventBindingsPlaceholder = "__VEIL_PANEL_EVENT_BINDINGS__"

type Slice struct {
	Name          string
	RenderSlots   []RenderSlot
	EventBindings []EventBinding
}

type SliceCatalog struct {
	runtimes []service.ManagedRuntime
}

func NewSliceCatalog(runtimes []service.ManagedRuntime) SliceCatalog {
	out := make([]service.ManagedRuntime, len(runtimes))
	copy(out, runtimes)
	return SliceCatalog{runtimes: out}
}

func (c SliceCatalog) Slices() []Slice {
	return []Slice{
		{
			Name:        "intro",
			RenderSlots: []RenderSlot{{Placeholder: panelIntroCardsPlaceholder, Render: panelIntroCardsHTML}, {Placeholder: panelIntroActionsPlaceholder, Render: panelIntroActionsJS}},
		},
		{
			Name: "service-status",
			RenderSlots: []RenderSlot{
				{Placeholder: ServiceStatusCardPlaceholder, Render: func() string { return ServiceStatusCardHTML(c.runtimes) }},
				{Placeholder: ServiceStatusActionsPlaceholder, Render: ServiceStatusActionsJS},
				{Placeholder: ServiceRestartActionsPlaceholder, Render: func() string { return ServiceRestartActionsJS(c.runtimes) }},
			},
			EventBindings: []EventBinding{{ElementID: "load-service-status", Handler: "loadServiceStatus", Event: "click"}},
		},
		{
			Name:        "runtime-stats",
			RenderSlots: []RenderSlot{{Placeholder: panelRuntimeStatsCardsPlaceholder, Render: panelRuntimeStatsCardsHTML}, {Placeholder: panelRuntimeStatsActionsPlaceholder, Render: panelRuntimeStatsActionsJS}},
		},
		{
			Name:        "client-links",
			RenderSlots: []RenderSlot{{Placeholder: panelClientLinksCardPlaceholder, Render: panelClientLinksCardHTML}, {Placeholder: panelClientLinksActionsPlaceholder, Render: panelClientLinksActionsJS}},
			EventBindings: []EventBinding{
				{ElementID: "load-client-links", Handler: "loadClientLinks", Event: "click"},
				{ElementID: "open-client-links-modal", Handler: "openClientLinksModal", Event: "click"},
				{ElementID: "load-client-subscription", Handler: "loadClientSubscription", Event: "click"},
				{ElementID: "load-client-subscription-raw", Handler: "loadRawClientSubscription", Event: "click"},
				{ElementID: "download-client-links-json", Handler: "downloadClientLinksJSON", Event: "click"},
				{ElementID: "download-client-configs", Handler: "downloadClientConfigArtifacts", Event: "click"},
				{ElementID: "copy-client-links", Handler: "copyClientLinksOutput", Event: "click"},
			},
		},

		{
			Name: "inbounds",
			RenderSlots: []RenderSlot{
				{Placeholder: panelInboundFormPlaceholder, Render: panelInboundFormHTML},
				{Placeholder: panelInboundActionsPlaceholder, Render: panelInboundActionsJS},
				{Placeholder: panelDynamicFieldsPlaceholder, Render: panelDynamicFieldsJS},
				{Placeholder: panelClientProfileControlsPlaceholder, Render: panelClientProfileControlsHTML},
				{Placeholder: panelClientProfileActionsPlaceholder, Render: panelClientProfileActionsJS},
			},
			EventBindings: []EventBinding{
				{ElementID: "inbound-protocol", Handler: "syncInboundTransportOptions", Event: "change"},
				{ElementID: "inbound-form", Handler: "saveInbound", Event: "submit"},
				{ElementID: "delete-inbound", Handler: "deleteInbound", Event: "click"},
				{ElementID: "load-inbounds", Handler: "loadInboundsIntoOutput", Event: "click"},
			},
		},
		{
			Name:        "routing",
			RenderSlots: []RenderSlot{{Placeholder: panelRoutingCardPlaceholder, Render: panelRoutingCardHTML}, {Placeholder: panelRoutingActionsPlaceholder, Render: panelRoutingActionsJS}},
			EventBindings: []EventBinding{
				{ElementID: "routing-rule-form", Handler: "saveRoutingRule", Event: "submit"},
				{ElementID: "delete-routing-rule", Handler: "deleteRoutingRule", Event: "click"},
				{ElementID: "apply-routing-preset", Handler: "applyRoutingPreset", Event: "click"},
			},
		},
		{
			Name:        "warp",
			RenderSlots: []RenderSlot{{Placeholder: panelWarpCardPlaceholder, Render: panelWarpCardHTML}, {Placeholder: panelWarpActionsPlaceholder, Render: panelWarpActionsJS}},
			EventBindings: []EventBinding{
				{ElementID: "warp-enabled", Handler: "applyWarpToggle", Event: "change"},
				{ElementID: "warp-form", Handler: "saveWarpConfig", Event: "submit"},
				{ElementID: "load-warp-config", Handler: "loadWarpIntoForm", Event: "click"},
			},
		},
		{
			Name:        "apply",
			RenderSlots: []RenderSlot{{Placeholder: ApplyCardPlaceholder, Render: ApplyCardHTML}, {Placeholder: ApplyActionsPlaceholder, Render: ApplyActionsJS}},
		},
		{
			Name: "diagnostics",
			RenderSlots: []RenderSlot{
				{Placeholder: DiagnosticsCardsPlaceholder, Render: func() string { return DiagnosticsCardsHTML(c.runtimes) }},
				{Placeholder: DiagnosticsActionsPlaceholder, Render: DiagnosticsActionsJS},
			},
		},
		{
			Name: "backups",
			RenderSlots: []RenderSlot{
				{Placeholder: BackupsCardPlaceholder, Render: BackupsCardHTML},
				{Placeholder: BackupsActionsPlaceholder, Render: BackupsActionsJS},
			},
			EventBindings: []EventBinding{
				{ElementID: "btn-create-backup", Handler: "createBackup", Event: "click"},
				{ElementID: "btn-load-backups", Handler: "loadBackups", Event: "click"},
				{ElementID: "btn-prune-backups", Handler: "pruneBackups", Event: "click"},
			},
		},
		{
			Name: "users",
			RenderSlots: []RenderSlot{
				{Placeholder: UsersCardPlaceholder, Render: UsersCardHTML},
				{Placeholder: UsersActionsPlaceholder, Render: UsersActionsJS},
			},
			EventBindings: []EventBinding{
				{ElementID: "user-form", Handler: "saveUser", Event: "submit"},
				{ElementID: "btn-cancel-user-edit", Handler: "cancelUserEdit", Event: "click"},
				{ElementID: "btn-load-sessions", Handler: "loadSessions", Event: "click"},
				{ElementID: "btn-generate-api-token", Handler: "generateReplacementAPIToken", Event: "click"},
				{ElementID: "btn-copy-generated-api-token", Handler: "copyGeneratedAPIToken", Event: "click"},
			},
		},
		{
			Name:        "utility",
			RenderSlots: []RenderSlot{{Placeholder: panelUtilityActionsPlaceholder, Render: panelUtilityActionsJS}, {Placeholder: EventBindingsPlaceholder, Render: func() string { return EventBindingsJS(c.EventBindings()) }}},
		},
	}
}

func (c SliceCatalog) Slice(name string) (Slice, bool) {
	for _, slice := range c.Slices() {
		if slice.Name == name {
			return slice, true
		}
	}
	return Slice{}, false
}

func (c SliceCatalog) RenderSlots() []RenderSlot {
	var slots []RenderSlot
	for _, slice := range c.Slices() {
		slots = append(slots, slice.RenderSlots...)
	}
	return slots
}

func (c SliceCatalog) EventBindings() []EventBinding {
	var bindings []EventBinding
	for _, slice := range c.Slices() {
		bindings = append(bindings, slice.EventBindings...)
	}
	return bindings
}

func EventBindingsJS(bindings []EventBinding) string {
	return `    document.querySelectorAll('[data-load]').forEach((button) => {
      button.addEventListener('click', () => loadJSON(button.dataset.load, button.dataset.output));
    });
` + RenderEventBindings(bindings) + `    document.getElementById('download-client-subscription').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=base64', 'veil-subscription.txt'));
    document.getElementById('download-client-subscription-raw').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=raw', 'veil-subscription-raw.txt'));
    syncInboundTransportOptions();

    // Auto-load settings and service status on panel open.
    loadServiceStatus();`
}
