package api

const panelServiceRestartActionsPlaceholder = "__VEIL_PANEL_SERVICE_RESTART_ACTIONS__"

func panelServiceRestartActionsJS() string {
	return `    document.getElementById('restart-veil').addEventListener('click', async () => {
      await loadJSON('/api/services/veil/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
      loadServiceStatus();
    });
    document.getElementById('restart-caddy').addEventListener('click', async () => {
      await loadJSON('/api/services/caddy/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });
    document.getElementById('restart-hysteria2').addEventListener('click', async () => {
      await loadJSON('/api/services/hysteria2/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });`
}
