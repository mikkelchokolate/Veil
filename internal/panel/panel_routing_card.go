package panel

const panelRoutingCardPlaceholder = "__VEIL_PANEL_ROUTING_CARD__"

func panelRoutingCardHTML() string {
	return `      <div class="card">
        <h2>Routing rules</h2>
        <p>List, create, update, or delete routing rules through <code>/api/routing/rules</code>.</p>
        <form id="routing-rule-form">
          <div class="form-grid">
            <div>
              <label for="routing-rule-name">Name</label>
              <input id="routing-rule-name" autocomplete="off" placeholder="non-ru-through-warp">
            </div>
            <div>
              <label for="routing-rule-match">Match</label>
              <input id="routing-rule-match" autocomplete="off" placeholder="geosite:geolocation-!ru">
            </div>
            <div>
              <label for="routing-rule-outbound">Outbound</label>
              <select id="routing-rule-outbound">
                <option value="direct">direct</option>
                <option value="warp">warp</option>
              </select>
            </div>
            <div>
              <label for="routing-rule-enabled">Enabled</label>
              <input id="routing-rule-enabled" type="checkbox" checked> enabled
            </div>
          </div>
          <div class="actions">
            <button id="save-routing-rule" type="submit">Save routing rule</button>
            <button id="delete-routing-rule" class="danger" type="button">Delete routing rule</button>
            <button class="secondary" type="button" data-load="/api/routing/rules" data-output="routing-output">Load routing</button>
          </div>
        </form>
        <div class="form-grid">
          <div>
            <label for="routing-preset-profile">Preset profile</label>
            <select id="routing-preset-profile">
              <option value="all">all</option>
              <option value="all-except-Russia">all-except-Russia</option>
              <option value="RU-blocked">RU-blocked</option>
            </select>
          </div>
          <div>
            <label>Rules source</label>
            <p class="hint">Russian geo/site data is pulled from runetfreedom/russia-v2ray-rules-dat when a Russia-aware preset is applied.</p>
          </div>
        </div>
        <div class="actions">
          <button id="apply-routing-preset" class="secondary" type="button">Apply routing preset</button>
          <button class="secondary" type="button" data-load="/api/routing/presets" data-output="routing-output">Load presets</button>
        </div>
        <pre id="routing-output">Not loaded</pre>
      </div>`
}
