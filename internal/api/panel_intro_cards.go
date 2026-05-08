package api

const panelIntroCardsPlaceholder = "__VEIL_PANEL_INTRO_CARDS__"

func panelIntroCardsHTML() string {
	return `    <h1>Veil Panel</h1>
    <div class="card">
      <p>Web panel for ` + NewInboundProtocolCatalog().DisplayNameList() + ` management. Use the sections below to configure and apply settings.</p>
      <p>Status API: <code>/api/status</code> &middot; Version: <code>/api/version</code> &middot; Firewall: <code>/api/firewall</code> &middot; Health: <code>/healthz</code> &middot; Metrics: <code>/metrics</code> &middot; System: <code>/api/system</code> &middot; Network: <code>/api/network</code> &middot; DNS: <code>/api/tools/dns-lookup</code> &middot; Ping: <code>/api/tools/ping</code> &middot; Profile preview: <code>/api/profiles/ru-recommended/preview</code></p>
    </div>
    <div class="card">
      <h2>Version</h2>
      <p>Veil server version and runtime info from <code>/api/version</code>.</p>
      <button id="load-version" type="button">Load version</button>
      <pre id="version-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>API token</h2>
      <p>If the server was started with <code>--auth-token</code> or <code>VEIL_API_TOKEN</code>, paste the token here. The browser stores it only in <code>localStorage</code> and sends it as <code>X-Veil-Token</code>.</p>
      <label for="api-token">Token</label>
      <input id="api-token" type="password" autocomplete="off" placeholder="Optional API token">
    </div>
    <div class="card">
      <h2>Profile preview</h2>
      <p>Preview a <code>ru-recommended</code> install profile without writing anything. Domain and email are required only for NaiveProxy/Hysteria2 stacks.</p>
      <form id="profile-preview-form">
        <div class="form-grid">
          <div>
            <label for="profile-domain">Domain</label>
            <input id="profile-domain" autocomplete="off" placeholder="vpn.example.com">
          </div>
          <div>
            <label for="profile-email">Email</label>
            <input id="profile-email" type="email" autocomplete="off" placeholder="admin@example.com">
          </div>
          <div>
            <label for="profile-stack">Stack</label>
            <select id="profile-stack">
` + panelStackOptionsHTML() + `            </select>
          </div>
        </div>
        <div class="actions">
          <button id="preview-profile" type="submit">Preview profile</button>
        </div>
      </form>
      <pre id="profile-preview-output">Not generated</pre>
    </div>`
}
