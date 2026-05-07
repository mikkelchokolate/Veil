package api

const panelServiceStatusActionsPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_ACTIONS__"

func panelServiceStatusActionsJS() string {
	return `    function loadServiceStatus() {
      return loadJSON('/api/status', 'service-status-output');
    }

    // Auto-refresh for service status (10s interval)
    let autoRefreshInterval = null;
    document.getElementById('toggle-auto-refresh').addEventListener('click', function() {
      const btn = this;
      if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
        btn.textContent = 'Auto-refresh: OFF';
        btn.classList.remove('danger');
        btn.classList.add('secondary');
      } else {
        loadServiceStatus(); // immediate refresh
        autoRefreshInterval = setInterval(loadServiceStatus, 10000);
        btn.textContent = 'Auto-refresh: ON (10s)';
        btn.classList.remove('secondary');
        btn.classList.add('danger');
      }
    });
    // Clean up interval on page unload
    window.addEventListener('beforeunload', function() {
      if (autoRefreshInterval) clearInterval(autoRefreshInterval);
    });`
}
