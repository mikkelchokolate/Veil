package api

const panelEventBindingsPlaceholder = "__VEIL_PANEL_EVENT_BINDINGS__"

func panelEventBindingsJS() string {
	return `    document.querySelectorAll('[data-load]').forEach((button) => {
      button.addEventListener('click', () => loadJSON(button.dataset.load, button.dataset.output));
    });
    document.getElementById('settings-form').addEventListener('submit', saveSettings);
    document.getElementById('load-settings').addEventListener('click', loadSettingsIntoForm);
    document.getElementById('load-service-status').addEventListener('click', loadServiceStatus);
    document.getElementById('load-client-links').addEventListener('click', loadClientLinks);
    document.getElementById('load-client-subscription').addEventListener('click', loadClientSubscription);
    document.getElementById('load-client-subscription-raw').addEventListener('click', loadRawClientSubscription);
    document.getElementById('download-client-subscription').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=base64', 'veil-subscription.txt'));
    document.getElementById('download-client-subscription-raw').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=raw', 'veil-subscription-raw.txt'));
    document.getElementById('download-mieru-configs').addEventListener('click', downloadMieruConfigs);
    document.getElementById('copy-client-links').addEventListener('click', copyClientLinksOutput);
    document.getElementById('inbound-form').addEventListener('submit', saveInbound);
    document.getElementById('delete-inbound').addEventListener('click', deleteInbound);
    document.getElementById('load-inbounds').addEventListener('click', loadInboundsIntoOutput);
    document.getElementById('routing-rule-form').addEventListener('submit', saveRoutingRule);
    document.getElementById('delete-routing-rule').addEventListener('click', deleteRoutingRule);
    document.getElementById('apply-routing-preset').addEventListener('click', applyRoutingPreset);
    document.getElementById('warp-form').addEventListener('submit', saveWarpConfig);
    document.getElementById('load-warp-config').addEventListener('click', loadWarpIntoForm);

    // Auto-load settings and service status on panel open.
    loadSettingsIntoForm();
    loadServiceStatus();`
}
