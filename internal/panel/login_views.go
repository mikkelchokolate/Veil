package panel

import "strings"

// LoginHTML renders the login page. It reuses the exact same CSS variables,
// fonts, and component classes defined in panelHTMLBase so the two pages are
// visually identical.
func LoginHTML(basePath string, locale string) string {
	html := loginHTMLTemplate
	html = strings.ReplaceAll(html, "__PROMETHEUS_BG_BASE64__", prometheusBgBase64)
	html = strings.ReplaceAll(html, "__VEIL_LOCALE__", NormalizeLocale(locale))
	html = strings.ReplaceAll(html, "__VEIL_LOCALIZATION_RUNTIME__", LocalizationRuntimeJS())
	if basePath == "" || basePath == "/" {
		return html
	}
	bp := strings.TrimRight(basePath, "/")
	replacer := strings.NewReplacer(
		`"/api/`, `"`+bp+`/api/`,
		`'/api/`, `'`+bp+`/api/`,
	)
	return replacer.Replace(html)
}

// loginHTMLTemplate shares the design tokens, fonts, and card component
// styling with panelHTMLBase.
const loginHTMLTemplate = `<!doctype html>
<html lang="__VEIL_LOCALE__">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Login – Veil Panel</title>
  <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='%23ffe6cb'%3E%3Cpath d='M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm-9 12H5v-2h6v2zm4-4H5v-2h10v2zm4-4H5V6h14v2z'/%3E%3C/svg%3E">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root {
      --canvas: #060d0d;
      --surface: #0a1414;
      --primary: #ffe6cb;
      --accent: #8b5cf6;
      --accent-success: #10b981;
      --accent-danger: #ef4444;
      --accent-warning: #f59e0b;
      --border: color-mix(in srgb, var(--primary) 15%, transparent);
      --border-hover: color-mix(in srgb, var(--primary) 30%, transparent);
      --bg-hover: color-mix(in srgb, var(--primary) 5%, transparent);
      --text-main: var(--primary);
      --text-muted: color-mix(in srgb, var(--primary) 60%, transparent);
      --text-disabled: color-mix(in srgb, var(--primary) 30%, transparent);
    }

    html {
      background-color: var(--canvas);
      scrollbar-gutter: stable;
    }
    body {
      margin: 0;
      font-family: 'Inter', system-ui, -apple-system, sans-serif;
      background-color: transparent;
      color: var(--text-main);
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      width: 100vw;
      overflow: hidden;
    }

    /* grain overlay – same as main panel */
    body::after {
      content: "";
      position: fixed;
      inset: 0;
      z-index: 999999;
      pointer-events: none;
      opacity: 0.28;
      background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noiseFilter'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.95' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noiseFilter)'/%3E%3C/svg%3E");
      mix-blend-mode: overlay;
    }

    /* background image – same as main panel */
    .bg-overlay-container {
      position: fixed;
      inset: 0;
      z-index: -1;
      pointer-events: none;
      background-color: var(--canvas);
    }
    .bg-image {
      position: absolute;
      inset: 0;
      background-image: url('data:image/jpeg;base64,__PROMETHEUS_BG_BASE64__');
      background-size: cover;
      background-position: left center;
      background-repeat: no-repeat;
      opacity: 0.08;
      filter: saturate(25%) contrast(130%) brightness(85%);
    }

    /* ── card component ── */
    .card {
      background: color-mix(in srgb, var(--surface) 40%, transparent);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 32px;
      width: 100%;
      max-width: 400px;
      box-sizing: border-box;
      transition: border-color 0.2s ease;
    }
    .card:hover {
      border-color: var(--border-hover);
    }
    .card h2 {
      margin-top: 0;
      font-size: 1.1rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.08rem;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      color: #fff;
      mix-blend-mode: plus-lighter;
      border-bottom: 1px solid var(--border);
      padding-bottom: 16px;
      margin-bottom: 24px;
    }

    /* ── form elements ── */
    label {
      display: block;
      margin-bottom: 8px;
      font-size: 0.8rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      color: var(--text-muted);
      font-weight: 500;
    }
    input, select {
      box-sizing: border-box;
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 12px 16px;
      background: color-mix(in srgb, var(--canvas) 60%, transparent);
      backdrop-filter: blur(8px);
      -webkit-backdrop-filter: blur(8px);
      color: var(--text-main);
      font-family: inherit;
      font-size: 0.9rem;
      transition: border-color 0.2s;
    }
    input:focus {
      outline: none;
      border-color: var(--primary);
    }
    .locale-picker {
      display: flex;
      align-items: center;
      gap: 10px;
      margin: 0 0 20px;
    }
    .locale-picker span {
      color: var(--text-muted);
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.75rem;
      text-transform: uppercase;
    }
    .locale-picker select {
      width: auto;
      margin-left: auto;
      padding: 8px 10px;
      background: var(--canvas);
    }

    /* ── button ── */
    button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 14px 20px;
      background: transparent;
      color: var(--primary);
      font-family: 'JetBrains Mono', monospace;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08rem;
      font-size: 0.8rem;
      cursor: pointer;
      transition: background-color 0.2s, border-color 0.2s, color 0.2s;
      width: 100%;
      box-sizing: border-box;
    }
    button:hover {
      background: var(--bg-hover);
      border-color: var(--primary);
    }
    button:active {
      transform: scale(0.98);
    }

    .pulse-static {
      display: inline-block;
      width: 6px;
      height: 6px;
      background-color: var(--primary);
    }

    .error-msg {
      color: var(--accent-danger);
      background: color-mix(in srgb, var(--accent-danger) 10%, transparent);
      border: 1px solid var(--accent-danger);
      padding: 12px 14px;
      font-size: 0.78rem;
      font-family: 'JetBrains Mono', monospace;
      margin-bottom: 20px;
      display: none;
      box-sizing: border-box;
    }
  </style>
</head>
<body>
  <div class="bg-overlay-container">
    <div class="bg-image"></div>
  </div>

  <div class="card">
    <label class="locale-picker">
      <span>Language</span>
      <select data-veil-locale-select aria-label="Language">
        <option value="en">English</option>
        <option value="ru">Русский</option>
      </select>
    </label>
    <h2><span class="pulse-static"></span>&nbsp;Veil Panel</h2>

    <div id="error" class="error-msg"></div>

    <form id="login-form">
      <div style="margin-bottom: 20px;">
        <label for="username">Username</label>
        <input type="text" id="username" autocomplete="username" required placeholder="Enter username">
      </div>
      <div style="margin-bottom: 20px;">
        <label for="password">Password</label>
        <input type="password" id="password" autocomplete="current-password" required placeholder="Enter password">
      </div>
      <button type="submit">Log In</button>
    </form>
  </div>

  <script>
    window.veilLocale = "__VEIL_LOCALE__";
    window.veil_csrf_token = "";
__VEIL_LOCALIZATION_RUNTIME__
    document.getElementById('login-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const username = document.getElementById('username').value.trim();
      const password = document.getElementById('password').value;
      const errorDiv = document.getElementById('error');
      errorDiv.style.display = 'none';
      try {
        const resp = await fetch("/api/auth/login", {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, password }),
        });
        const data = await resp.json();
        if (!resp.ok) {
          errorDiv.textContent = data.message || veilT('auth.failed');
          errorDiv.style.display = 'block';
          return;
        }
        localStorage.setItem('veil_csrf_token', data.csrfToken);
        localStorage.setItem('veil_username', data.username);
        localStorage.setItem('veil_user_role', data.role || '');
        localStorage.removeItem('veil_api_token');
        if (data.locale) {
          document.cookie = 'veil_locale=' + encodeURIComponent(data.locale) + '; Path=/; Max-Age=31536000; SameSite=Lax';
        }
        window.location.reload();
      } catch (err) {
        errorDiv.textContent = String(err);
        errorDiv.style.display = 'block';
      }
    });
  </script>
</body>
</html>
`
