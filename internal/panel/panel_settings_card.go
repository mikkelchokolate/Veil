package panel

const panelSettingsCardPlaceholder = "__VEIL_PANEL_SETTINGS_CARD__"

func panelSettingsCardHTML() string {
	return `      <div class="card" id="settings-card">
        <h2>Settings</h2>
        <p>Panel/global settings endpoint: <code>/api/settings</code></p>
        <p class="hint">Protocol settings are rendered from plugin schemas. Redacted passwords are preserved by the API when saved back as <code>[REDACTED]</code>.</p>
        <form id="settings-form">
          <div class="form-grid">
            <div>
              <label for="settings-panel-listen">Panel listen</label>
              <input id="settings-panel-listen" required autocomplete="off" placeholder="127.0.0.1:2096">
            </div>
            <div>
              <label for="settings-mode">Mode</label>
              <input id="settings-mode" required autocomplete="off" placeholder="server">
            </div>
            <div>
              <label for="settings-panel-access">Panel access</label>
              <select id="settings-panel-access">
                <option value="local">local</option>
                <option value="direct">direct</option>
                <option value="caddy">caddy</option>
              </select>
            </div>
            <div>
              <label for="settings-web-base-path">Web base path</label>
              <input id="settings-web-base-path" autocomplete="off" placeholder="/panel-secret/">
            </div>
            <div>
              <label for="settings-domain">Domain</label>
              <input id="settings-domain" autocomplete="off" placeholder="vpn.example.com">
            </div>
            <div>
              <label for="settings-email">Email</label>
              <input id="settings-email" type="email" autocomplete="off" placeholder="admin@example.com">
            </div>
          </div>
          <div id="settings-protocol-fields" style="display:flex;flex-direction:column;gap:16px;border-top:1px solid var(--border);padding-top:16px;margin-top:16px;">
            <p class="hint">Loading protocol settings…</p>
          </div>
          <div class="actions">
            <button id="save-settings" type="submit">Save settings</button>
            <button class="secondary" id="load-settings" type="button">Load settings</button>
          </div>
        </form>
      <pre id="settings-output" role="status" aria-live="polite">Not loaded</pre>
      </div>`
}
