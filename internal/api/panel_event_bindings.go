package api

const panelEventBindingsPlaceholder = "__VEIL_PANEL_EVENT_BINDINGS__"

func panelEventBindingsJS() string {
	return `    document.querySelectorAll('[data-load]').forEach((button) => {
      button.addEventListener('click', () => loadJSON(button.dataset.load, button.dataset.output));
    });
` + panelEventBindingCatalogJS() + `    document.getElementById('download-client-subscription').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=base64', 'veil-subscription.txt'));
    document.getElementById('download-client-subscription-raw').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=raw', 'veil-subscription-raw.txt'));
    syncInboundTransportOptions();

    // Auto-load settings and service status on panel open.
    loadSettingsIntoForm();
    loadServiceStatus();`
}
