package panel

const panelRuntimeStatsActionsPlaceholder = "__VEIL_PANEL_RUNTIME_STATS_ACTIONS__"

func panelRuntimeStatsActionsJS() string {
	return `    document.getElementById('load-system-stats').addEventListener('click', async () => {
      await loadJSON('/api/system', 'system-stats-output');
    });
    document.getElementById('load-network-stats').addEventListener('click', async () => {
      await loadJSON('/api/network', 'network-stats-output');
    });
    document.getElementById('load-connections-stats').addEventListener('click', async () => {
      await loadJSON('/api/connections', 'connections-stats-output');
    });
    document.getElementById('load-processes-stats').addEventListener('click', async () => {
      await loadJSON('/api/processes', 'processes-stats-output');
    });
    document.getElementById('load-disk-stats').addEventListener('click', async () => {
      await loadJSON('/api/disk', 'disk-stats-output');
    });`
}
