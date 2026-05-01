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
    .card { border: 1px solid #263043; border-radius: 16px; padding: 24px; margin: 16px 0; background: #0d111c; }
    code { color: #8be9fd; }
    button { border: 0; border-radius: 10px; padding: 10px 14px; background: #4f46e5; color: white; cursor: pointer; }
    pre { overflow: auto; border-radius: 10px; padding: 12px; background: #070a12; color: #c9d1d9; }
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
      <h2>Speedtest</h2>
      <p>Run server-side speedtest-cli/Ookla speedtest from the panel.</p>
      <button id="run-speedtest" type="button">Run speedtest</button>
      <pre id="speedtest-output">Not started</pre>
    </div>
  </main>
  <script>
    const output = document.getElementById('speedtest-output');
    document.getElementById('run-speedtest').addEventListener('click', async () => {
      output.textContent = 'Running speedtest...';
      try {
        const response = await fetch('/api/tools/speedtest', { method: 'POST' });
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return;
        }
        const result = JSON.parse(text);
        output.textContent = [
          'Server: ' + (result.server || 'n/a'),
          'Ping: ' + result.pingMs + ' ms',
          'Download: ' + result.downloadMbps + ' Mbps',
          'Upload: ' + result.uploadMbps + ' Mbps'
        ].join('\n');
      } catch (err) {
        output.textContent = String(err);
      }
    });
  </script>
</body>
</html>
`
