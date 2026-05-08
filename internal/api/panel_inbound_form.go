package api

const panelInboundFormPlaceholder = "__VEIL_PANEL_INBOUND_FORM__"

// panelInboundFormHTML renders the Panel Module slice for Inbound management.
// Client profile controls are kept behind their own placeholder so the nested
// Module can evolve without forcing callers to understand the whole Inbound form.
func panelInboundFormHTML() string {
	return `      <div class="card">
        <h2>Inbounds</h2>
        <p>Create, update, or delete NaiveProxy, Hysteria2, and Mieru inbound definitions through <code>/api/inbounds</code>.</p>
        <form id="inbound-form">
          <div class="form-grid">
            <div>
              <label for="inbound-name">Name</label>
              <input id="inbound-name" autocomplete="off" placeholder="naive">
            </div>
            <div>
              <label for="inbound-protocol">Protocol</label>
              <select id="inbound-protocol">
` + panelInboundProtocolOptionsHTML() + `              </select>
            </div>
            <div>
              <label for="inbound-transport">Transport</label>
              <select id="inbound-transport">
` + panelInboundTransportOptionsHTML() + `              </select>
            </div>
            <div>
              <label for="inbound-port">Port</label>
              <input id="inbound-port" type="number" min="1" max="65535" placeholder="443">
            </div>
            <div>
              <label for="inbound-password">Password</label>
              <div style="display:flex;gap:8px">
                <input id="inbound-password" type="text" autocomplete="off" placeholder="auto-generated if empty" style="flex:1">
                <button type="button" class="secondary" onclick="genInboundPassword()" style="white-space:nowrap">Generate</button>
              </div>
            </div>
            <div>
              <label for="inbound-enabled">Enabled</label>
              <input id="inbound-enabled" type="checkbox" checked> enabled
            </div>
            <div style="grid-column: 1 / -1">
__VEIL_PANEL_CLIENT_PROFILE_CONTROLS__
            </div>
          </div>
          <div class="actions">
            <button id="save-inbound" type="submit">Save inbound</button>
            <button id="delete-inbound" class="danger" type="button">Delete inbound</button>
            <button class="secondary" id="load-inbounds" type="button">Load inbounds</button>
          </div>
        </form>
        <pre id="inbounds-output">Not loaded</pre>
      </div>`
}
