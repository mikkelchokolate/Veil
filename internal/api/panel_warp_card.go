package api

const panelWarpCardPlaceholder = "__VEIL_PANEL_WARP_CARD__"

func panelWarpCardHTML() string {
	return `      <div class="card">
        <h2>WARP</h2>
        <p>Configure the optional sing-box WireGuard/WARP sidecar through <code>/api/warp</code>.</p>
        <p class="hint">Redacted private/license keys are preserved by the API when saved back as [REDACTED].</p>
        <form id="warp-form">
          <div class="form-grid">
            <div>
              <label for="warp-enabled">Enabled</label>
              <input id="warp-enabled" type="checkbox"> enabled
            </div>
            <div>
              <label for="warp-endpoint">Endpoint</label>
              <input id="warp-endpoint" autocomplete="off" placeholder="engage.cloudflareclient.com:2408">
            </div>
            <div>
              <label for="warp-local-address">Local address</label>
              <input id="warp-local-address" autocomplete="off" placeholder="172.16.0.2/32">
            </div>
            <div>
              <label for="warp-peer-public-key">Peer public key</label>
              <input id="warp-peer-public-key" autocomplete="off" placeholder="Cloudflare peer public key">
            </div>
            <div>
              <label for="warp-private-key">Private key</label>
              <input id="warp-private-key" type="password" autocomplete="off" placeholder="WireGuard private key">
            </div>
            <div>
              <label for="warp-license-key">License key</label>
              <input id="warp-license-key" type="password" autocomplete="off" placeholder="Optional WARP+ license">
            </div>
            <div>
              <label for="warp-reserved">Reserved bytes</label>
              <input id="warp-reserved" autocomplete="off" placeholder="1,2,3">
            </div>
            <div>
              <label for="warp-socks-listen">SOCKS listen</label>
              <input id="warp-socks-listen" autocomplete="off" placeholder="127.0.0.1">
            </div>
            <div>
              <label for="warp-socks-port">SOCKS port</label>
              <input id="warp-socks-port" type="number" min="1" max="65535" placeholder="40000">
            </div>
            <div>
              <label for="warp-mtu">MTU</label>
              <input id="warp-mtu" type="number" min="576" max="9000" placeholder="1280">
            </div>
          </div>
          <div class="actions">
            <button id="save-warp-config" type="submit">Save WARP config</button>
            <button class="secondary" id="load-warp-config" type="button">Load WARP</button>
          </div>
        </form>
        <pre id="warp-output">Not loaded</pre>
      </div>`
}
