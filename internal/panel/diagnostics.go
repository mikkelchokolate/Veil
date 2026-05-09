package panel

import (
	"strings"

	"github.com/veil-panel/veil/internal/service"
)

const DiagnosticsCardsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_CARDS__"
const DiagnosticsActionsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_ACTIONS__"

func DiagnosticsCardsHTML(runtimes []service.ManagedRuntime) string {
	return `    <div class="card">
      <h2>Speedtest</h2>
      <p>Run server-side speedtest-cli/Ookla speedtest from the panel.</p>
      <button id="run-speedtest" type="button">Run speedtest</button>
      <pre id="speedtest-output">Not started</pre>
    </div>
    <div class="card">
      <h2>DNS lookup</h2>
      <p>Resolve a hostname from the server using <code>/api/tools/dns-lookup</code>.</p>
      <div class="form-grid">
        <div>
          <label for="dns-hostname">Hostname</label>
          <input id="dns-hostname" autocomplete="off" placeholder="example.com">
        </div>
      </div>
      <div class="actions">
        <button id="run-dns-lookup" type="button">Lookup</button>
      </div>
      <pre id="dns-lookup-output">Not started</pre>
    </div>
    <div class="card">
      <h2>Ping</h2>
      <p>Ping a host from the server using <code>/api/tools/ping</code>.</p>
      <div class="form-grid">
        <div>
          <label for="ping-host">Host</label>
          <input id="ping-host" autocomplete="off" placeholder="8.8.8.8">
        </div>
        <div>
          <label for="ping-count">Count (1-10)</label>
          <input id="ping-count" type="number" min="1" max="10" value="3">
        </div>
      </div>
      <div class="actions">
        <button id="run-ping" type="button">Ping</button>
      </div>
      <pre id="ping-output">Not started</pre>
    </div>
    <div class="card">
      <h2>Firewall</h2>
      <p>Check UFW firewall status and planned rules from <code>/api/firewall</code>.</p>
      <button id="load-firewall" type="button">Load firewall</button>
      <pre id="firewall-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Service logs</h2>
      <p>View recent journald logs for managed services.</p>
      <div class="form-grid">
        <div>
          <label for="log-unit">Service unit</label>
          <select id="log-unit">
` + ManagedLogUnitOptionsHTML(runtimes) + `          </select>
        </div>
        <div>
          <label for="log-lines">Lines</label>
          <input id="log-lines" type="number" min="1" max="500" value="50">
        </div>
      </div>
      <div class="actions">
        <button id="load-logs" type="button">Load logs</button>
      </div>
      <pre id="logs-output" style="max-height: 400px; overflow-y: auto;">Not loaded</pre>
    </div>`
}

func ManagedLogUnitOptionsHTML(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	for _, runtime := range runtimes {
		unitName := strings.TrimSuffix(runtime.Unit, ".service")
		b.WriteString(`            <option value="`)
		b.WriteString(unitName)
		b.WriteString(`">`)
		b.WriteString(runtime.Name)
		b.WriteString(" (")
		b.WriteString(runtime.Unit)
		b.WriteString(")</option>\n")
	}
	return b.String()
}

func DiagnosticsActionsJS() string {
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
