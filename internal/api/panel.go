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
    .card { border: 1px solid #263043; border-radius: 16px; padding: 24px; background: #0d111c; }
    code { color: #8be9fd; }
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
  </main>
</body>
</html>
`
