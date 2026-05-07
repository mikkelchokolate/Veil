package api

const panelServiceStatusCardPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_CARD__"

func panelServiceStatusCardHTML() string {
	return `    <div class="card">
      <h2>Service status</h2>
      <p>Read live systemd state for Veil, NaiveProxy/Caddy, Hysteria2, and WARP/sing-box through <code>/api/status</code>.</p>
      <div class="actions">
        <button id="load-service-status" type="button">Load service status</button>
        <button id="toggle-auto-refresh" class="secondary" type="button">Auto-refresh: OFF</button>
      </div>
      <pre id="service-status-output">Not loaded</pre>
      <div class="actions">
        <button id="restart-veil" class="danger" type="button">Restart veil</button>
        <button id="restart-caddy" class="danger" type="button">Restart caddy</button>
        <button id="restart-hysteria2" class="danger" type="button">Restart hysteria2</button>
      </div>
    </div>`
}
