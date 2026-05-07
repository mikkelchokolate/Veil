package api

const panelDiagnosticsActionsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_ACTIONS__"

func panelDiagnosticsActionsJS() string {
	return `    document.getElementById('run-speedtest').addEventListener('click', async () => {
      await loadJSON('/api/tools/speedtest', 'speedtest-output', { method: 'POST' });
    });

    // Service logs
    document.getElementById('load-logs').addEventListener('click', async () => {
      const unit = document.getElementById('log-unit').value;
      const lines = document.getElementById('log-lines').value || '50';
      await loadJSON('/api/logs?unit=' + encodeURIComponent(unit) + '&lines=' + encodeURIComponent(lines), 'logs-output');
      // Extract the output field for nicer display
      try {
        const el = document.getElementById('logs-output');
        const data = JSON.parse(el.textContent);
        if (data && data.output) {
          el.textContent = data.output;
        }
      } catch (_) {
        // keep raw JSON if parsing fails
      }
    });

    // Firewall
    document.getElementById('load-firewall').addEventListener('click', async () => {
      await loadJSON('/api/firewall', 'firewall-output');
    });

    // DNS lookup
    document.getElementById('run-dns-lookup').addEventListener('click', async () => {
      const hostname = document.getElementById('dns-hostname').value.trim();
      if (!hostname) {
        document.getElementById('dns-lookup-output').textContent = 'Hostname is required';
        return;
      }
      await loadJSON('/api/tools/dns-lookup', 'dns-lookup-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostname })
      });
    });

    // Ping
    document.getElementById('run-ping').addEventListener('click', async () => {
      const host = document.getElementById('ping-host').value.trim();
      const count = document.getElementById('ping-count').value || '3';
      if (!host) {
        document.getElementById('ping-output').textContent = 'Host is required';
        return;
      }
      await loadJSON('/api/tools/ping', 'ping-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, count: Number(count) })
      });
    });`
}
