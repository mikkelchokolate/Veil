package api

const panelDiagnosticsCardsPlaceholder = "__VEIL_PANEL_DIAGNOSTICS_CARDS__"

func panelDiagnosticsCardsHTML() string {
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
            <option value="veil">veil</option>
            <option value="caddy">caddy (NaiveProxy)</option>
            <option value="hysteria2">hysteria2</option>
            <option value="sing-box">sing-box (WARP)</option>
          </select>
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
