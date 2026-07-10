package panel

import (
	"html"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/service"
)

const DiagnosticsCardsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_CARDS__"
const DiagnosticsActionsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_ACTIONS__"

func DiagnosticsCardsHTML(runtimes []service.ManagedRuntime) string {
	return `    <div class="card">
      <h2>Speedtest</h2>
      <p>Run server-side speedtest-cli/Ookla speedtest from the panel.</p>
      <div class="actions">
        <button id="run-speedtest" type="button">Run speedtest</button>
      </div>
      
      <div class="terminal-window">
        <div class="terminal-header">
          <div class="terminal-title">speedtest-cli</div>
        </div>
            <pre class="terminal-body" id="speedtest-output" role="status" aria-live="polite" style="color: #60a5fa;">Not started</pre>
      </div>
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
        <button id="run-dns-lookup" type="button">Lookup hostname</button>
      </div>
      
      <div class="terminal-window">
        <div class="terminal-header">
          <div class="terminal-title">nslookup</div>
        </div>
            <pre class="terminal-body" id="dns-lookup-output" role="status" aria-live="polite" style="color: #a78bfa;">Not started</pre>
      </div>
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
      
      <div class="terminal-window">
        <div class="terminal-header">
          <div class="terminal-title">ping</div>
        </div>
            <pre class="terminal-body" id="ping-output" role="status" aria-live="polite">Not started</pre>
      </div>
    </div>

    <div class="card">
      <h2>Firewall</h2>
      <p>Check UFW firewall status and planned rules from <code>/api/firewall</code>.</p>
      <div class="actions">
        <button id="load-firewall" type="button">Load firewall</button>
      </div>
      
      <div class="terminal-window">
        <div class="terminal-header">
          <div class="terminal-title">ufw status</div>
        </div>
            <pre class="terminal-body" id="firewall-output" role="status" aria-live="polite" style="color: #fbbf24;">Not loaded</pre>
      </div>
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
      
      <div class="terminal-window">
        <div class="terminal-header">
          <div class="terminal-title">journalctl</div>
        </div>
            <pre class="terminal-body" id="logs-output" role="status" aria-live="polite" style="max-height: 400px; overflow-y: auto; color: #34d399;">Not loaded</pre>
      </div>
    </div>`
}

func ManagedLogUnitOptionsHTML(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	for _, runtime := range runtimes {
		unitName := html.EscapeString(strings.TrimSuffix(runtime.Unit, ".service"))
		displayName := html.EscapeString(runtime.Name)
		displayUnit := html.EscapeString(runtime.Unit)
		b.WriteString(`            <option value="`)
		b.WriteString(unitName)
		b.WriteString(`">`)
		b.WriteString(displayName)
		b.WriteString(" (")
		b.WriteString(displayUnit)
		b.WriteString(")</option>\n")
	}
	return b.String()
}

func DiagnosticsActionsJS() string {
	return `    async function runDiagnosticAction(buttonID, action) {
      const button = document.getElementById(buttonID);
      if (!button || button.dataset.diagnosticInFlight === 'true') return null;
      button.dataset.diagnosticInFlight = 'true';
      button.disabled = true;
      try {
        return await action();
      } finally {
        delete button.dataset.diagnosticInFlight;
        button.disabled = false;
      }
    }

    function diagnosticIntegerValue(id, fallback) {
      const input = document.getElementById(id);
      const raw = input && input.value.trim() ? input.value.trim() : String(fallback);
      if (!input || !input.checkValidity()) {
        if (input) input.reportValidity();
        return null;
      }
      const value = Number(raw);
      if (!Number.isInteger(value)) {
        input.setCustomValidity('Enter a whole number.');
        input.reportValidity();
        input.setCustomValidity('');
        return null;
      }
      return value;
    }

    document.getElementById('run-speedtest').addEventListener('click', async () => {
      await runDiagnosticAction('run-speedtest', () => loadJSON('/api/tools/speedtest', 'speedtest-output', { method: 'POST' }));
    });

    // Service logs
    document.getElementById('load-logs').addEventListener('click', async () => {
      const unit = document.getElementById('log-unit').value;
      const lines = diagnosticIntegerValue('log-lines', 50);
      if (lines === null) return;
      await runDiagnosticAction('load-logs', async () => {
        const data = await loadJSON('/api/logs?unit=' + encodeURIComponent(unit) + '&lines=' + encodeURIComponent(lines), 'logs-output');
        if (data && data.output) {
          document.getElementById('logs-output').textContent = data.output;
        }
        return data;
      });
    });

    // Firewall
    document.getElementById('load-firewall').addEventListener('click', async () => {
      await runDiagnosticAction('load-firewall', () => loadJSON('/api/firewall', 'firewall-output'));
    });

    // DNS lookup
    document.getElementById('run-dns-lookup').addEventListener('click', async () => {
      const hostname = document.getElementById('dns-hostname').value.trim();
      if (!hostname) {
        document.getElementById('dns-lookup-output').textContent = veilT('diagnostics.hostnameRequired');
        return;
      }
      await runDiagnosticAction('run-dns-lookup', () => loadJSON('/api/tools/dns-lookup', 'dns-lookup-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostname })
      }));
    });

    // Ping
    document.getElementById('run-ping').addEventListener('click', async () => {
      const host = document.getElementById('ping-host').value.trim();
      const count = diagnosticIntegerValue('ping-count', 3);
      if (!host) {
        document.getElementById('ping-output').textContent = veilT('diagnostics.hostRequired');
        return;
      }
      if (count === null) return;
      await runDiagnosticAction('run-ping', () => loadJSON('/api/tools/ping', 'ping-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, count })
      }));
    });`
}
