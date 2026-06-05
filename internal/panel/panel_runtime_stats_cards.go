package panel

const panelRuntimeStatsCardsPlaceholder = "__VEIL_PANEL_RUNTIME_STATS_CARDS__"

func panelRuntimeStatsCardsHTML() string {
	return `    <!-- Telemetry Circles Grid -->
    <div class="telemetry-grid">
      <div class="telemetry-card">
        <h3>CPU Usage</h3>
        <div class="circle-chart-container">
          <svg class="circle-chart" viewBox="0 0 120 120">
            <circle class="circle-chart__background" cx="60" cy="60" r="50" fill="none" stroke="#202a42" stroke-width="8" />
            <circle id="cpu-circle" class="circle-chart__circle" cx="60" cy="60" r="50" fill="none" stroke="url(#cpu-grad)" stroke-width="8" stroke-dasharray="314" stroke-dashoffset="314" stroke-linecap="round" />
            <defs>
              <linearGradient id="cpu-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#818cf8" />
                <stop offset="100%" stop-color="#4f46e5" />
              </linearGradient>
            </defs>
          </svg>
          <div class="circle-chart__info">
            <span id="cpu-text" class="percent-val">0.0%</span>
            <span class="percent-label">Load</span>
          </div>
        </div>
        <div class="telemetry-details" id="cpu-load-details">1m/5m/15m: 0.00 / 0.00 / 0.00</div>
      </div>

      <div class="telemetry-card">
        <h3>Memory Usage</h3>
        <div class="circle-chart-container">
          <svg class="circle-chart" viewBox="0 0 120 120">
            <circle class="circle-chart__background" cx="60" cy="60" r="50" fill="none" stroke="#202a42" stroke-width="8" />
            <circle id="mem-circle" class="circle-chart__circle" cx="60" cy="60" r="50" fill="none" stroke="url(#mem-grad)" stroke-width="8" stroke-dasharray="314" stroke-dashoffset="314" stroke-linecap="round" />
            <defs>
              <linearGradient id="mem-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#34d399" />
                <stop offset="100%" stop-color="#059669" />
              </linearGradient>
            </defs>
          </svg>
          <div class="circle-chart__info">
            <span id="mem-text" class="percent-val">0.0%</span>
            <span class="percent-label">RAM</span>
          </div>
        </div>
        <div class="telemetry-details" id="mem-usage-details">0 MB / 0 MB</div>
      </div>

      <div class="telemetry-card">
        <h3>Disk Space</h3>
        <div class="circle-chart-container">
          <svg class="circle-chart" viewBox="0 0 120 120">
            <circle class="circle-chart__background" cx="60" cy="60" r="50" fill="none" stroke="#202a42" stroke-width="8" />
            <circle id="disk-circle" class="circle-chart__circle" cx="60" cy="60" r="50" fill="none" stroke="url(#disk-grad)" stroke-width="8" stroke-dasharray="314" stroke-dashoffset="314" stroke-linecap="round" />
            <defs>
              <linearGradient id="disk-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#fbbf24" />
                <stop offset="100%" stop-color="#d97706" />
              </linearGradient>
            </defs>
          </svg>
          <div class="circle-chart__info">
            <span id="disk-text" class="percent-val">0.0%</span>
            <span class="percent-label">Disk</span>
          </div>
        </div>
        <div class="telemetry-details" id="disk-usage-details">0.0 GB / 0.0 GB</div>
      </div>
    </div>

    <!-- Core Runtime stats boxes -->
    <div class="card">
      <h2>System resources</h2>
      <p>CPU, memory, disk, load average from <code>/api/system</code>.</p>
      <div style="display: flex; gap: 8px;">
        <button id="load-system-stats" type="button">Refresh telemetry</button>
        <button onclick="toggleRawView('system-stats-output')" class="secondary" type="button">Raw JSON</button>
      </div>
      <pre id="system-stats-output" role="status" aria-live="polite" style="display: none; margin-top: 16px;">Not loaded</pre>
    </div>

    <div class="card">
      <h2>Network interfaces</h2>
      <p>RX/TX bytes and packets from <code>/api/network</code>.</p>
      <div class="table-container" id="net-interfaces-table" style="display: none; margin-bottom: 16px;">
        <table>
          <thead>
            <tr>
              <th>Interface</th>
              <th>Bytes Received (RX)</th>
              <th>Bytes Transmitted (TX)</th>
              <th>Packets (RX / TX)</th>
            </tr>
          </thead>
          <tbody id="net-interfaces-tbody">
            <tr><td colspan="4" style="text-align: center; color: var(--text-muted);">No interfaces loaded. Click refresh.</td></tr>
          </tbody>
        </table>
      </div>
      <div style="display: flex; gap: 8px;">
        <button id="load-network-stats" type="button">Load network stats</button>
        <button onclick="toggleRawView('network-stats-output')" class="secondary" type="button">Raw JSON</button>
      </div>
      <pre id="network-stats-output" role="status" aria-live="polite" style="display: none; margin-top: 16px;">Not loaded</pre>
    </div>

    <div class="card">
      <h2>Listening ports</h2>
      <p>TCP/UDP listening sockets from <code>/api/connections</code>.</p>
      <div id="connections-list" style="display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0;"></div>
      <div style="display: flex; gap: 8px;">
        <button id="load-connections-stats" type="button">Load connections</button>
        <button onclick="toggleRawView('connections-stats-output')" class="secondary" type="button">Raw JSON</button>
      </div>
      <pre id="connections-stats-output" role="status" aria-live="polite" style="display: none; margin-top: 16px;">Not loaded</pre>
    </div>

    <div class="card">
      <h2>Managed processes</h2>
      <p>Running service processes from <code>/api/processes</code>.</p>
      <div class="table-container" id="processes-table" style="display: none; margin-bottom: 16px;">
        <table>
          <thead>
            <tr>
              <th>PID</th>
              <th>Name</th>
              <th>CPU %</th>
              <th>Memory (RSS)</th>
              <th>Uptime</th>
            </tr>
          </thead>
          <tbody id="processes-tbody">
            <tr><td colspan="5" style="text-align: center; color: var(--text-muted);">No processes loaded.</td></tr>
          </tbody>
        </table>
      </div>
      <div style="display: flex; gap: 8px;">
        <button id="load-processes-stats" type="button">Load processes</button>
        <button onclick="toggleRawView('processes-stats-output')" class="secondary" type="button">Raw JSON</button>
      </div>
      <pre id="processes-stats-output" role="status" aria-live="polite" style="display: none; margin-top: 16px;">Not loaded</pre>
    </div>

    <div class="card">
      <h2>Disk usage</h2>
      <p>Directory sizes for Veil paths from <code>/api/disk</code>.</p>
      <div id="disk-paths-container" style="margin: 16px 0; display: flex; flex-direction: column; gap: 12px;"></div>
      <div style="display: flex; gap: 8px;">
        <button id="load-disk-stats" type="button">Load disk usage</button>
        <button onclick="toggleRawView('disk-stats-output')" class="secondary" type="button">Raw JSON</button>
      </div>
      <pre id="disk-stats-output" role="status" aria-live="polite" style="display: none; margin-top: 16px;">Not loaded</pre>
    </div>`
}
