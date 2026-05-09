package panel

const panelSettingsCardPlaceholder = "__VEIL_PANEL_SETTINGS_CARD__"

func panelSettingsCardHTML() string {
	return `      <div class="card">
        <h2>Settings</h2>
        <p>Panel/global settings endpoint: <code>/api/settings</code></p>
        <p class="hint">Redacted proxy passwords are preserved by the API when saved back as [REDACTED].</p>
        <form id="settings-form">
          <div class="form-grid">
            <div>
              <label for="settings-panel-listen">Panel listen</label>
              <input id="settings-panel-listen" autocomplete="off" placeholder="127.0.0.1:2096">
            </div>
            <div>
              <label for="settings-mode">Mode</label>
              <input id="settings-mode" autocomplete="off" placeholder="server">
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
            <div>
              <label for="settings-naive-username">Naive username</label>
              <input id="settings-naive-username" autocomplete="off" placeholder="veil">
            </div>
            <div>
              <label for="settings-naive-password">Naive password</label>
              <input id="settings-naive-password" type="password" autocomplete="off" placeholder="NaiveProxy password">
            </div>
            <div>
              <label for="settings-hysteria2-password">Hysteria2 password</label>
              <input id="settings-hysteria2-password" type="password" autocomplete="off" placeholder="Hysteria2 password">
            </div>
            <div>
              <label for="settings-masquerade-url">Masquerade URL</label>
              <input id="settings-masquerade-url" autocomplete="off" placeholder="https://example.com">
            </div>
            <div>
              <label for="settings-fallback-root">Fallback root</label>
              <input id="settings-fallback-root" autocomplete="off" placeholder="/var/lib/veil/www">
            </div>
          </div>
          <div class="actions">
            <button id="save-settings" type="submit">Save settings</button>
            <button class="secondary" id="load-settings" type="button">Load settings</button>
          </div>
        </form>
        <pre id="settings-output">Not loaded</pre>
      </div>`
}
