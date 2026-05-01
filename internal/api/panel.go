package api

const panelHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Veil Panel</title>
  <style>
    body { margin: 0; font-family: Inter, system-ui, sans-serif; background: #070a12; color: #e6edf3; }
    main { max-width: 960px; margin: 0 auto; padding: 48px 24px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 16px; }
    .card { border: 1px solid #263043; border-radius: 16px; padding: 24px; margin: 16px 0; background: #0d111c; }
    code { color: #8be9fd; }
    label { display: block; margin-bottom: 8px; color: #9fb0c3; }
    input { box-sizing: border-box; width: 100%; border: 1px solid #263043; border-radius: 10px; padding: 10px 12px; background: #070a12; color: #e6edf3; }
    button { border: 0; border-radius: 10px; padding: 10px 14px; background: #4f46e5; color: white; cursor: pointer; }
    pre { overflow: auto; border-radius: 10px; padding: 12px; background: #070a12; color: #c9d1d9; min-height: 72px; }
  </style>
</head>
<body>
  <main>
    <h1>Veil Panel</h1>
    <div class="card">
      <p>Early web panel shell for NaiveProxy TCP + Hysteria2 UDP management.</p>
      <p>Status API: <code>/api/status</code></p>
      <p>Health: <code>/healthz</code></p>
    </div>
    <div class="card">
      <h2>API token</h2>
      <p>If the server was started with <code>--auth-token</code> or <code>VEIL_API_TOKEN</code>, paste the token here. The browser stores it only in <code>localStorage</code> and sends it as <code>X-Veil-Token</code>.</p>
      <label for="api-token">Token</label>
      <input id="api-token" type="password" autocomplete="off" placeholder="Optional API token">
    </div>
    <div class="grid">
      <div class="card">
        <h2>Settings</h2>
        <p>Panel/global settings endpoint: <code>/api/settings</code></p>
        <button type="button" data-load="/api/settings" data-output="settings-output">Load settings</button>
        <pre id="settings-output">Not loaded</pre>
      </div>
      <div class="card">
        <h2>Inbounds</h2>
        <p>NaiveProxy and Hysteria2 inbound definitions: <code>/api/inbounds</code></p>
        <button type="button" data-load="/api/inbounds" data-output="inbounds-output">Load inbounds</button>
        <pre id="inbounds-output">Not loaded</pre>
      </div>
      <div class="card">
        <h2>Routing rules</h2>
        <p>Routing rules endpoint: <code>/api/routing/rules</code></p>
        <button type="button" data-load="/api/routing/rules" data-output="routing-output">Load routing</button>
        <pre id="routing-output">Not loaded</pre>
      </div>
      <div class="card">
        <h2>WARP</h2>
        <p>WARP outbound configuration: <code>/api/warp</code></p>
        <button type="button" data-load="/api/warp" data-output="warp-output">Load WARP</button>
        <pre id="warp-output">Not loaded</pre>
      </div>
    </div>
    <div class="card">
      <h2>Apply plan</h2>
      <p>Validate current management state and show staged config/reload actions before any real service changes: <code>/api/apply/plan</code></p>
      <button id="build-apply-plan" type="button">Build apply plan</button>
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

    async function loadJSON(path, outputId, options) {
      const output = document.getElementById(outputId);
      output.textContent = 'Loading ' + path + '...';
      const requestOptions = options || {};
      requestOptions.headers = Object.assign({}, requestOptions.headers || {}, authHeaders());
      try {
        const response = await fetch(path, requestOptions);
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return;
        }
        output.textContent = JSON.stringify(JSON.parse(text), null, 2);
      } catch (err) {
        output.textContent = String(err);
      }
    }

    document.querySelectorAll('[data-load]').forEach((button) => {
      button.addEventListener('click', () => loadJSON(button.dataset.load, button.dataset.output));
    });

    document.getElementById('build-apply-plan').addEventListener('click', async () => {
      await loadJSON('/api/apply/plan', 'apply-plan-output', { method: 'POST' });
    });

    document.getElementById('run-speedtest').addEventListener('click', async () => {
      await loadJSON('/api/tools/speedtest', 'speedtest-output', { method: 'POST' });
    });
  </script>
</body>
</html>
`
