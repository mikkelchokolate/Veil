package api

import "strings"

// panelHTMLBase is the raw panel HTML. Paths in JS strings are all /-prefixed
// (e.g., "/api/status"). At serve time, a replacer injects the web base path.
const panelHTMLBase = `<!doctype html>
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
    input, select, textarea { box-sizing: border-box; width: 100%; border: 1px solid #263043; border-radius: 10px; padding: 10px 12px; background: #070a12; color: #e6edf3; }
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
__VEIL_PANEL_INTRO_CARDS__
__VEIL_PANEL_SERVICE_STATUS_CARD__
__VEIL_PANEL_RUNTIME_STATS_CARDS__
__VEIL_PANEL_CLIENT_LINKS_CARD__

    <div class="grid">
__VEIL_PANEL_SETTINGS_CARD__
__VEIL_PANEL_INBOUND_FORM__
    </div>

    <div class="grid">
__VEIL_PANEL_ROUTING_CARD__

__VEIL_PANEL_WARP_CARD__
    </div>

__VEIL_PANEL_APPLY_CARD__
__VEIL_PANEL_DIAGNOSTICS_CARDS__
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

__VEIL_PANEL_SETTINGS_ACTIONS__

    async function loadInboundsIntoOutput() {
      await loadJSON('/api/inbounds', 'inbounds-output');
    }

__VEIL_PANEL_SERVICE_STATUS_ACTIONS__

__VEIL_PANEL_CLIENT_LINKS_ACTIONS__

    function randomPassword() {
      const bytes = new Uint8Array(9);
      crypto.getRandomValues(bytes);
      return btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    }

    function genInboundPassword() {
      document.getElementById('inbound-password').value = randomPassword();
    }

__VEIL_PANEL_CLIENT_PROFILE_ACTIONS__

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
      const profilesRaw = document.getElementById('inbound-profiles').value.trim();
      if (profilesRaw) {
        try {
          payload.profiles = JSON.parse(profilesRaw);
        } catch (err) {
          document.getElementById('inbounds-output').textContent = 'Client profiles must be valid JSON: ' + String(err);
          return;
        }
      }
      const pw = document.getElementById('inbound-password').value.trim();
      const inbounds = await loadJSON('/api/inbounds', 'inbounds-output');
      const exists = Array.isArray(inbounds) && inbounds.some((inbound) => inbound.name === name);
      if (pw) {
        payload.password = pw;
      } else if (!exists && !payload.profiles) {
        // Auto-generate password for new single-profile Inbounds.
        // Client profile passwords are generated by the backend when profiles are provided.
        genInboundPassword();
        payload.password = document.getElementById('inbound-password').value;
      }
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
    // Service restart buttons
    document.getElementById('restart-veil').addEventListener('click', async () => {
      await loadJSON('/api/services/veil/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
      loadServiceStatus();
    });
    document.getElementById('restart-caddy').addEventListener('click', async () => {
      await loadJSON('/api/services/caddy/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });
    document.getElementById('restart-hysteria2').addEventListener('click', async () => {
      await loadJSON('/api/services/hysteria2/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });
    document.getElementById('load-system-stats').addEventListener('click', async () => {
      await loadJSON('/api/system', 'system-stats-output');
    });
    document.getElementById('load-network-stats').addEventListener('click', async () => {
      await loadJSON('/api/network', 'network-stats-output');
    });
    document.getElementById('load-connections-stats').addEventListener('click', async () => {
      await loadJSON('/api/connections', 'connections-stats-output');
    });
    document.getElementById('load-processes-stats').addEventListener('click', async () => {
      await loadJSON('/api/processes', 'processes-stats-output');
    });
    document.getElementById('load-disk-stats').addEventListener('click', async () => {
      await loadJSON('/api/disk', 'disk-stats-output');
    });
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

__VEIL_PANEL_APPLY_ACTIONS__

    document.getElementById('run-speedtest').addEventListener('click', async () => {
      await loadJSON('/api/tools/speedtest', 'speedtest-output', { method: 'POST' });
    });

    // Service logs
    document.getElementById('load-logs').addEventListener('click', async () => {
      const unit = document.getElementById('log-unit').value;
      const lines = document.getElementById('log-lines').value || '50';
      await loadJSON('/api/logs?unit=' + encodeURIComponent(unit) + '&lines=' + encodeURIComponent(lines), 'logs-output');
      // Extract the output field for nicer display
      try {
        const el = document.getElementById('logs-output');
        const data = JSON.parse(el.textContent);
        if (data && data.output) {
          el.textContent = data.output;
        }
      } catch (_) {
        // keep raw JSON if parsing fails
      }
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

    // Version
    document.getElementById('load-version').addEventListener('click', async () => {
      await loadJSON('/api/version', 'version-output');
    });

    // Firewall
    document.getElementById('load-firewall').addEventListener('click', async () => {
      await loadJSON('/api/firewall', 'firewall-output');
    });

    // DNS lookup
    document.getElementById('run-dns-lookup').addEventListener('click', async () => {
      const hostname = document.getElementById('dns-hostname').value.trim();
      if (!hostname) {
        document.getElementById('dns-lookup-output').textContent = 'Hostname is required';
        return;
      }
      await loadJSON('/api/tools/dns-lookup', 'dns-lookup-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostname })
      });
    });

    // Ping
    document.getElementById('run-ping').addEventListener('click', async () => {
      const host = document.getElementById('ping-host').value.trim();
      const count = document.getElementById('ping-count').value || '3';
      if (!host) {
        document.getElementById('ping-output').textContent = 'Host is required';
        return;
      }
      await loadJSON('/api/tools/ping', 'ping-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, count: Number(count) })
      });
    });

    // Auto-load settings and service status on panel open.
    loadSettingsIntoForm();
    loadServiceStatus();
  </script>
</body>
</html>
`

// panelHTML returns the panel HTML with all API paths adjusted for the given base path.
// When basePath is "/", the HTML is returned unchanged.
func panelHTML(basePath string) string {
	html := renderPanelHTMLBase()
	if basePath == "" || basePath == "/" {
		return html
	}
	// basePath is like "/secret/" — strip trailing slash for replacement.
	bp := strings.TrimRight(basePath, "/")
	replacer := strings.NewReplacer(
		`"/api/`, `"`+bp+`/api/`,
		`'/api/`, `'`+bp+`/api/`,
		`"/healthz`, `"`+bp+`/healthz`,
		`'/healthz`, `'`+bp+`/healthz`,
		`"/metrics`, `"`+bp+`/metrics`,
		`'/metrics`, `'`+bp+`/metrics`,
	)
	return replacer.Replace(html)
}
