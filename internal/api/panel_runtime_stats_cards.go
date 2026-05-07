package api

const panelRuntimeStatsCardsPlaceholder = "__VEIL_PANEL_RUNTIME_STATS_CARDS__"

func panelRuntimeStatsCardsHTML() string {
	return `    <div class="card">
      <h2>System resources</h2>
      <p>CPU, memory, disk, load average from <code>/api/system</code>.</p>
      <button id="load-system-stats" type="button">Load system stats</button>
      <pre id="system-stats-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Network interfaces</h2>
      <p>RX/TX bytes and packets from <code>/api/network</code>.</p>
      <button id="load-network-stats" type="button">Load network stats</button>
      <pre id="network-stats-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Listening ports</h2>
      <p>TCP/UDP listening sockets from <code>/api/connections</code>.</p>
      <button id="load-connections-stats" type="button">Load connections</button>
      <pre id="connections-stats-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Managed processes</h2>
      <p>Running service processes from <code>/api/processes</code>.</p>
      <button id="load-processes-stats" type="button">Load processes</button>
      <pre id="processes-stats-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Disk usage</h2>
      <p>Directory sizes for Veil paths from <code>/api/disk</code>.</p>
      <button id="load-disk-stats" type="button">Load disk usage</button>
      <pre id="disk-stats-output">Not loaded</pre>
    </div>`
}
