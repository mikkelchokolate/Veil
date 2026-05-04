package api

const panelHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Veil Panel</title>
  <style>
    body { margin: 0; font-family: Inter, system-ui, sans-serif; background: #070a12; color: #e6edf3; }
    main { max-width: 1180px; margin: 0 auto; padding: 48px 24px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; }
    .card { border: 1px solid #263043; border-radius: 16px; padding: 24px; margin: 16px 0; background: #0d111c; }
    .form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin: 12px 0; }
    .actions { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
    code { color: #8be9fd; }
    label { display: block; margin-bottom: 8px; color: #9fb0c3; }
    input, select { box-sizing: border-box; width: 100%; border: 1px solid #263043; border-radius: 10px; padding: 10px 12px; background: #070a12; color: #e6edf3; }
    input[type="checkbox"] { width: auto; margin-right: 8px; }
    button { border: 0; border-radius: 10px; padding: 10px 14px; background: #4f46e5; color: white; cursor: pointer; }
    button.secondary { background: #334155; }
    button.danger { background: #dc2626; }
    pre { overflow: auto; border-radius: 10px; padding: 12px; background: #070a12; color: #c9d1d9; min-height: 72px; }
    .hint { color: #9fb0c3; font-size: 0.92rem; }
  </style>
</head>
<body>
  <main>
    <h1>Veil Panel</h1>
    <div class="card">
      <p>Web panel for NaiveProxy TCP + Hysteria2 UDP management. Use the sections below to configure and apply settings.</p>
      <p>Status API: <code>/api/status</code> &middot; Health: <code>/healthz</code> &middot; Profile preview: <code>/api/profiles/ru-recommended/preview</code></p>
    </div>
    <div class="card">
      <h2>API token</h2>
      <p>If the server was started with <code>--auth-token</code> or <code>VEIL_API_TOKEN</code>, paste the token here. The browser stores it only in <code>localStorage</code> and sends it as <code>X-Veil-Token</code>.</p>
      <label for="api-token">Token</label>
      <input id="api-token" type="password" autocomplete="off" placeholder="Optional API token">
    </div>
    <div class="card">
      <h2>Profile preview</h2>
      <p>Preview a <code>ru-recommended</code> install profile without writing anything — generated Caddyfile, Hysteria2 YAML, and client URIs.</p>
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
              <option value="both">both</option>
              <option value="naive">naive</option>
              <option value="hysteria2">hysteria2</option>
            </select>
          </div>
        </div>
        <div class="actions">
          <button id="preview-profile" type="submit">Preview profile</button>
        </div>
      </form>
      <pre id="profile-preview-output">Not generated</pre>
    </div>
    <div class="card">
      <h2>Service status</h2>
      <p>Read live systemd state for Veil, NaiveProxy/Caddy, Hysteria2, and WARP/sing-box through <code>/api/status</code>.</p>
      <button id="load-service-status" type="button">Load service status</button>
      <pre id="service-status-output">Not loaded</pre>
    </div>
    <div class="card">
      <h2>Client links</h2>
      <p>Generate current NaiveProxy and Hysteria2 client connection URIs from saved settings and enabled inbounds through <code>/api/client-links</code>.</p>
      <button id="load-client-links" type="button">Load client links</button>
      <button id="load-client-subscription" type="button">Load base64 subscription</button>
      <button id="load-client-subscription-raw" type="button">Load raw subscription</button>
      <button id="download-client-subscription" class="secondary" type="button">Download base64 subscription</button>
      <button id="download-client-subscription-raw" class="secondary" type="button">Download raw subscription</button>
      <button id="copy-client-links" class="secondary" type="button">Copy output</button>
      <pre id="client-links-output">Not loaded</pre>
    </div>

    <div class="grid">
      <div class="card">
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
              <label for="settings-stack">Stack</label>
              <select id="settings-stack">
                <option value="both">both</option>
                <option value="naive">naive</option>
                <option value="hysteria2">hysteria2</option>
              </select>
            </div>
            <div>
              <label for="settings-mode">Mode</label>
              <input id="settings-mode" autocomplete="off" placeholder="server">
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
      </div>
      <div class="card">
        <h2>Inbounds</h2>
        <p>Create, update, or delete NaiveProxy and Hysteria2 inbound definitions through <code>/api/inbounds</code>.</p>
        <form id="inbound-form">
          <div class="form-grid">
            <div>
              <label for="inbound-name">Name</label>
              <input id="inbound-name" autocomplete="off" placeholder="naive">
            </div>
            <div>
              <label for="inbound-protocol">Protocol</label>
              <select id="inbound-protocol">
                <option value="naiveproxy">naiveproxy</option>
                <option value="hysteria2">hysteria2</option>
              </select>
            </div>
            <div>
              <label for="inbound-transport">Transport</label>
              <select id="inbound-transport">
                <option value="tcp">tcp</option>
                <option value="udp">udp</option>
              </select>
            </div>
            <div>
              <label for="inbound-port">Port</label>
              <input id="inbound-port" type="number" min="1" max="65535" placeholder="443">
            </div>
            <div>
              <label for="inbound-enabled">Enabled</label>
              <input id="inbound-enabled" type="checkbox" checked> enabled
            </div>
          </div>
          <div class="actions">
            <button id="save-inbound" type="submit">Save inbound</button>
            <button id="delete-inbound" class="danger" type="button">Delete inbound</button>
            <button class="secondary" id="load-inbounds" type="button">Load inbounds</button>
          </div>
        </form>
        <pre id="inbounds-output">Not loaded</pre>
      </div>
    </div>

    <div class="grid">
      <div class="card">
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
      </div>

      <div class="card">
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
      </div>
    </div>

    <div class="card">
      <h2>Apply plan</h2>
      <p>Validate current management state and show staged config/reload actions before any real service changes: <code>/api/apply/plan</code></p>
      <p>Service reload also runs fixed health checks and automatically rolls live configs back if reload or health fails.</p>
      <button id="build-apply-plan" type="button">Build apply plan</button>
      <button id="apply-staged-files" type="button">Apply staged files</button>
      <button id="apply-live-configs" type="button">Apply live configs</button>
      <button id="reload-services" type="button">Reload and health check services</button>
      <div class="form-grid">
        <div>
          <label for="apply-history-stage">History stage</label>
          <select id="apply-history-stage">
            <option value="">all</option>
            <option value="staged">staged</option>
            <option value="live">live</option>
            <option value="services">services</option>
            <option value="validation">validation</option>
            <option value="rollback">rollback</option>
          </select>
        </div>
        <div>
          <label for="apply-history-success">History success</label>
          <select id="apply-history-success">
            <option value="">all</option>
            <option value="true">success</option>
            <option value="false">failed</option>
          </select>
        </div>
        <div>
          <label for="apply-history-limit">History limit</label>
          <input id="apply-history-limit" type="number" min="0" placeholder="50">
        </div>
      </div>
      <button id="load-apply-history" type="button">Load apply history</button>
      <pre id="apply-plan-output">Not planned</pre>
    </div>
    <div class="card">
      <h2>Speedtest</h2>
      <p>Run server-side speedtest-cli/Ookla speedtest from the panel.</p>
      <button id="run-speedtest" type="button">Run speedtest</button>
      <pre id="speedtest-output">Not started</pre>
    </div>
  </main>
  <script>
    const tokenInput = document.getElementById('api-token');
    tokenInput.value = localStorage.getItem('veil_api_token') || '';
    tokenInput.addEventListener('input', () => {
      localStorage.setItem('veil_api_token', tokenInput.value);
    });

    function authHeaders() {
      const token = localStorage.getItem('veil_api_token') || '';
      return token ? { 'X-Veil-Token': token } : {};
    }

    function requestHeaders(extra) {
      return Object.assign({}, extra || {}, authHeaders());
    }

    async function loadJSON(path, outputId, options) {
      const output = document.getElementById(outputId);
      output.textContent = 'Loading ' + path + '...';
      const requestOptions = options || {};
      requestOptions.headers = requestHeaders(requestOptions.headers || {});
      try {
        const response = await fetch(path, requestOptions);
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return null;
        }
        const parsed = text ? JSON.parse(text) : null;
        output.textContent = parsed === null ? 'OK' : JSON.stringify(parsed, null, 2);
        return parsed;
      } catch (err) {
        output.textContent = String(err);
        return null;
      }
    }

    function parseReserved(value) {
      if (!value.trim()) {
        return [];
      }
      return value.split(',').map((part) => Number(part.trim())).filter((value) => Number.isInteger(value));
    }

    function numberOrZero(id) {
      const value = document.getElementById(id).value;
      return value === '' ? 0 : Number(value);
    }

    async function loadSettingsIntoForm() {
      const data = await loadJSON('/api/settings', 'settings-output');
      if (!data) {
        return;
      }
      document.getElementById('settings-panel-listen').value = data.panelListen || '';
      document.getElementById('settings-stack').value = data.stack || 'both';
      document.getElementById('settings-mode').value = data.mode || '';
      document.getElementById('settings-domain').value = data.domain || '';
      document.getElementById('settings-email').value = data.email || '';
      document.getElementById('settings-naive-username').value = data.naiveUsername || '';
      document.getElementById('settings-naive-password').value = data.naivePassword || '';
      document.getElementById('settings-hysteria2-password').value = data.hysteria2Password || '';
      document.getElementById('settings-masquerade-url').value = data.masqueradeURL || '';
      document.getElementById('settings-fallback-root').value = data.fallbackRoot || '';
    }

    async function saveSettings(event) {
      event.preventDefault();
      await loadJSON('/api/settings', 'settings-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          panelListen: document.getElementById('settings-panel-listen').value,
          stack: document.getElementById('settings-stack').value,
          mode: document.getElementById('settings-mode').value,
          domain: document.getElementById('settings-domain').value,
          email: document.getElementById('settings-email').value,
          naiveUsername: document.getElementById('settings-naive-username').value,
          naivePassword: document.getElementById('settings-naive-password').value,
          hysteria2Password: document.getElementById('settings-hysteria2-password').value,
          masqueradeURL: document.getElementById('settings-masquerade-url').value,
          fallbackRoot: document.getElementById('settings-fallback-root').value
        })
      });
    }

    async function loadInboundsIntoOutput() {
      await loadJSON('/api/inbounds', 'inbounds-output');
    }

    function applyHistoryPath() {
      const params = new URLSearchParams();
      const stage = document.getElementById('apply-history-stage').value;
      const success = document.getElementById('apply-history-success').value;
      const limit = document.getElementById('apply-history-limit').value;
      if (stage) {
        params.set('stage', stage);
      }
      if (success) {
        params.set('success', success);
      }
      if (limit) {
        params.set('limit', limit);
      }
      const query = params.toString();
      return '/api/apply/history?' + query;
    }

    async function loadApplyHistory() {
      await loadJSON(applyHistoryPath(), 'apply-plan-output');
    }

    async function loadServiceStatus() {
      await loadJSON('/api/status', 'service-status-output');
    }

    async function loadClientLinks() {
      await loadJSON('/api/client-links', 'client-links-output');
    }

    async function loadClientSubscription() {
      await loadClientSubscriptionPath('/api/client-links/subscription?format=base64');
    }

    async function loadRawClientSubscription() {
      await loadClientSubscriptionPath('/api/client-links/subscription?format=raw');
    }

    async function loadClientSubscriptionPath(path) {
      const output = document.getElementById('client-links-output');
      output.textContent = 'Loading ' + path + '...';
      try {
        const response = await fetch(path, { headers: requestHeaders() });
        const text = await response.text();
        output.textContent = response.ok ? text : (text || ('HTTP ' + response.status));
      } catch (err) {
        output.textContent = String(err);
      }
    }

    async function copyClientLinksOutput() {
      const output = document.getElementById('client-links-output');
      const text = output.textContent || '';
      if (!text || text === 'Not loaded') {
        output.textContent = 'Nothing to copy yet';
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        output.textContent = text + '\n\nCopied to clipboard';
      } catch (err) {
        output.textContent = text + '\n\nCopy failed: ' + String(err);
      }
    }

    async function downloadClientSubscriptionPath(path, filename) {
      const output = document.getElementById('client-links-output');
      output.textContent = 'Downloading ' + path + '...';
      try {
        const response = await fetch(path, { headers: requestHeaders() });
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return;
        }
        const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        output.textContent = 'Downloaded ' + filename;
      } catch (err) {
        output.textContent = 'Download failed: ' + String(err);
      }
    }

    async function saveInbound(event) {
      event.preventDefault();
      const name = document.getElementById('inbound-name').value.trim();
      if (!name) {
        document.getElementById('inbounds-output').textContent = 'Inbound name is required';
        return;
      }
      const payload = {
        name: name,
        protocol: document.getElementById('inbound-protocol').value,
        transport: document.getElementById('inbound-transport').value,
        port: numberOrZero('inbound-port'),
        enabled: document.getElementById('inbound-enabled').checked
      };
      const inbounds = await loadJSON('/api/inbounds', 'inbounds-output');
      const exists = Array.isArray(inbounds) && inbounds.some((inbound) => inbound.name === name);
      await loadJSON(exists ? '/api/inbounds/' + encodeURIComponent(name) : '/api/inbounds', 'inbounds-output', {
        method: exists ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
    }

    async function deleteInbound() {
      const name = document.getElementById('inbound-name').value.trim();
      if (!name) {
        document.getElementById('inbounds-output').textContent = 'Inbound name is required';
        return;
      }
      await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
    }

    async function loadWarpIntoForm() {
      const data = await loadJSON('/api/warp', 'warp-output');
      if (!data) {
        return;
      }
      document.getElementById('warp-enabled').checked = Boolean(data.enabled);
      document.getElementById('warp-endpoint').value = data.endpoint || '';
      document.getElementById('warp-local-address').value = data.localAddress || '';
      document.getElementById('warp-peer-public-key').value = data.peerPublicKey || '';
      document.getElementById('warp-private-key').value = data.privateKey || '';
      document.getElementById('warp-license-key').value = data.licenseKey || '';
      document.getElementById('warp-reserved').value = Array.isArray(data.reserved) ? data.reserved.join(',') : '';
      document.getElementById('warp-socks-listen').value = data.socksListen || '';
      document.getElementById('warp-socks-port').value = data.socksPort || '';
      document.getElementById('warp-mtu').value = data.mtu || '';
    }

    async function saveWarpConfig(event) {
      event.preventDefault();
      await loadJSON('/api/warp', 'warp-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: document.getElementById('warp-enabled').checked,
          licenseKey: document.getElementById('warp-license-key').value,
          endpoint: document.getElementById('warp-endpoint').value,
          privateKey: document.getElementById('warp-private-key').value,
          localAddress: document.getElementById('warp-local-address').value,
          peerPublicKey: document.getElementById('warp-peer-public-key').value,
          reserved: parseReserved(document.getElementById('warp-reserved').value),
          socksListen: document.getElementById('warp-socks-listen').value,
          socksPort: numberOrZero('warp-socks-port'),
          mtu: numberOrZero('warp-mtu')
        })
      });
    }

    async function saveRoutingRule(event) {
      event.preventDefault();
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = 'Routing rule name is required';
        return;
      }
      const payload = {
        name: name,
        match: document.getElementById('routing-rule-match').value,
        outbound: document.getElementById('routing-rule-outbound').value,
        enabled: document.getElementById('routing-rule-enabled').checked
      };
      const rules = await loadJSON('/api/routing/rules', 'routing-output');
      const exists = Array.isArray(rules) && rules.some((rule) => rule.name === name);
      await loadJSON(exists ? '/api/routing/rules/' + encodeURIComponent(name) : '/api/routing/rules', 'routing-output', {
        method: exists ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
    }

    async function deleteRoutingRule() {
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = 'Routing rule name is required';
        return;
      }
      await loadJSON('/api/routing/rules/' + encodeURIComponent(name), 'routing-output', { method: 'DELETE' });
    }

    async function applyRoutingPreset() {
      const profile = document.getElementById('routing-preset-profile').value;
      await loadJSON('/api/routing/presets/' + encodeURIComponent(profile), 'routing-output', { method: 'POST' });
    }

    document.querySelectorAll('[data-load]').forEach((button) => {
      button.addEventListener('click', () => loadJSON(button.dataset.load, button.dataset.output));
    });
    document.getElementById('settings-form').addEventListener('submit', saveSettings);
    document.getElementById('load-settings').addEventListener('click', loadSettingsIntoForm);
    document.getElementById('load-service-status').addEventListener('click', loadServiceStatus);
    document.getElementById('load-client-links').addEventListener('click', loadClientLinks);
    document.getElementById('load-client-subscription').addEventListener('click', loadClientSubscription);
    document.getElementById('load-client-subscription-raw').addEventListener('click', loadRawClientSubscription);
    document.getElementById('download-client-subscription').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=base64', 'veil-subscription.txt'));
    document.getElementById('download-client-subscription-raw').addEventListener('click', () => downloadClientSubscriptionPath('/api/client-links/subscription?format=raw', 'veil-subscription-raw.txt'));
    document.getElementById('copy-client-links').addEventListener('click', copyClientLinksOutput);
    document.getElementById('inbound-form').addEventListener('submit', saveInbound);
    document.getElementById('delete-inbound').addEventListener('click', deleteInbound);
    document.getElementById('load-inbounds').addEventListener('click', loadInboundsIntoOutput);
    document.getElementById('routing-rule-form').addEventListener('submit', saveRoutingRule);
    document.getElementById('delete-routing-rule').addEventListener('click', deleteRoutingRule);
    document.getElementById('apply-routing-preset').addEventListener('click', applyRoutingPreset);
    document.getElementById('warp-form').addEventListener('submit', saveWarpConfig);
    document.getElementById('load-warp-config').addEventListener('click', loadWarpIntoForm);

    document.getElementById('build-apply-plan').addEventListener('click', async () => {
      await loadJSON('/api/apply/plan', 'apply-plan-output', { method: 'POST' });
    });

    document.getElementById('apply-staged-files').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });

    document.getElementById('apply-live-configs').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true, applyLive: true })
      });
    });

    document.getElementById('reload-services').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true, applyLive: true, applyServices: true })
      });
    });

    document.getElementById('load-apply-history').addEventListener('click', loadApplyHistory);

    document.getElementById('run-speedtest').addEventListener('click', async () => {
      await loadJSON('/api/tools/speedtest', 'speedtest-output', { method: 'POST' });
    });

    // Profile preview
    document.getElementById('profile-preview-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const domain = document.getElementById('profile-domain').value.trim();
      const email = document.getElementById('profile-email').value.trim();
      const stack = document.getElementById('profile-stack').value;
      if (!domain || !email) {
        document.getElementById('profile-preview-output').textContent = 'Domain and email are required';
        return;
      }
      await loadJSON('/api/profiles/ru-recommended/preview', 'profile-preview-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain, email, stack })
      });
    });

    // Auto-load settings and service status on panel open.
    loadSettingsIntoForm();
    loadServiceStatus();
  </script>
</body>
</html>
`
