package panel

import "strings"

func SetupHTML(basePath string, locale string) string {
	path := strings.TrimRight(basePath, "/")
	if path == "" {
		path = ""
	}
	html := strings.ReplaceAll(setupHTMLTemplate, "__VEIL_SETUP_API__", path+"/api/setup/complete")
	html = strings.ReplaceAll(html, "__VEIL_LOCALE__", NormalizeLocale(locale))
	return strings.ReplaceAll(html, "__VEIL_LOCALIZATION_RUNTIME__", LocalizationRuntimeJS())
}

const setupHTMLTemplate = `<!doctype html>
<html lang="__VEIL_LOCALE__">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Set up Veil</title>
  <style>
    :root {
      color-scheme: dark;
      --canvas: #080d0d;
      --surface: #101818;
      --surface-strong: #162121;
      --text: #f4eee5;
      --muted: #a7b0ab;
      --border: #34413f;
      --accent: #62c4a5;
      --danger: #ff7b72;
      --focus: #f2c96d;
    }
    * { box-sizing: border-box; }
    body {
      min-height: 100vh;
      margin: 0;
      display: grid;
      place-items: center;
      padding: 24px;
      background: var(--canvas);
      color: var(--text);
      font-family: Inter, ui-sans-serif, system-ui, sans-serif;
      letter-spacing: 0;
    }
    main {
      width: min(100%, 620px);
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--surface);
      padding: clamp(20px, 5vw, 38px);
      box-shadow: 0 24px 70px rgba(0, 0, 0, 0.36);
    }
    h1 { margin: 0 0 8px; font-size: 1.75rem; line-height: 1.2; }
    h2 { margin: 0 0 8px; font-size: 1rem; }
    p { margin: 0; color: var(--muted); line-height: 1.55; }
    form { display: grid; gap: 22px; margin-top: 28px; }
    section { display: grid; gap: 12px; }
    label { display: grid; gap: 7px; font-weight: 600; }
    input[type="text"], input[type="password"], select {
      width: 100%;
      min-height: 44px;
      border: 1px solid var(--border);
      border-radius: 6px;
      background: var(--surface-strong);
      color: var(--text);
      padding: 10px 12px;
      font: inherit;
    }
    input:focus-visible, select:focus-visible, button:focus-visible {
      outline: 3px solid var(--focus);
      outline-offset: 2px;
    }
    .locale-picker {
      display: flex;
      align-items: center;
      justify-content: flex-end;
      gap: 10px;
      margin-bottom: 20px;
      color: var(--muted);
    }
    .locale-picker select {
      width: auto;
      min-height: 40px;
      background: var(--surface-strong);
      color: var(--text);
      border: 1px solid var(--border);
      border-radius: 6px;
      padding: 8px 10px;
    }
    .choice {
      display: grid;
      grid-template-columns: 20px 1fr;
      align-items: start;
      gap: 10px;
      font-weight: 500;
    }
    .choice input { width: 18px; height: 18px; margin: 2px 0 0; }
    .notice {
      border-left: 3px solid var(--accent);
      padding: 10px 12px;
      background: var(--surface-strong);
      color: var(--muted);
      line-height: 1.5;
    }
    button {
      min-height: 44px;
      border: 1px solid var(--accent);
      border-radius: 6px;
      background: var(--accent);
      color: #07110e;
      padding: 10px 16px;
      font: inherit;
      font-weight: 750;
      cursor: pointer;
    }
    button:disabled { cursor: wait; opacity: 0.65; }
    #setup-result { min-height: 24px; color: var(--muted); }
    #setup-result.error { color: var(--danger); }
    @media (max-width: 480px) {
      body { padding: 0; place-items: stretch; }
      main { min-height: 100vh; border: 0; border-radius: 0; padding: 24px 18px; }
    }
    @media (prefers-reduced-motion: reduce) {
      *,
      *::before,
      *::after {
        animation-duration: 0.01ms !important;
        animation-iteration-count: 1 !important;
        transition-duration: 0.01ms !important;
      }
    }
  </style>
</head>
<body>
  <main>
    <label class="locale-picker">
      <span>Language</span>
      <select data-veil-locale-select aria-label="Language">
        <option value="en">English</option>
        <option value="ru">Русский</option>
      </select>
    </label>
    <h1>Set up Veil</h1>
    <p>Create the first administrator before configuring proxy inbounds.</p>
    <form id="setup-form">
      <section aria-labelledby="admin-heading">
        <h2 id="admin-heading">Create administrator</h2>
        <label for="setup-username">Username
          <input id="setup-username" name="username" type="text" minlength="3" maxlength="64" autocomplete="username" required>
        </label>
        <label for="setup-password">Password
          <input id="setup-password" name="password" type="password" minlength="12" autocomplete="new-password" required>
        </label>
      </section>
      <section aria-labelledby="access-heading">
        <h2 id="access-heading">Local access</h2>
        <div class="notice">Initial setup is available only on the loopback Panel. Public access stays disabled until authentication and TLS policy are satisfied.</div>
      </section>
      <section aria-labelledby="backup-heading">
        <h2 id="backup-heading">Backup and recovery</h2>
        <label class="choice" for="setup-backup-ack">
          <input id="setup-backup-ack" name="backupAcknowledged" type="checkbox" required>
          <span>I will preserve both the encrypted state and its state key in a protected backup.</span>
        </label>
      </section>
      <button id="setup-submit" type="submit">Complete setup</button>
      <p id="setup-result" aria-live="polite"></p>
    </form>
  </main>
  <script>
    window.veilLocale = "__VEIL_LOCALE__";
    window.veil_csrf_token = "";
__VEIL_LOCALIZATION_RUNTIME__
    let setupInFlight = false;
    document.getElementById('setup-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      if (setupInFlight) return;
      setupInFlight = true;
      let completed = false;
      const button = document.getElementById('setup-submit');
      const result = document.getElementById('setup-result');
      button.disabled = true;
      result.className = '';
      result.textContent = veilT('setup.saving');
      try {
        const response = await fetch("__VEIL_SETUP_API__", {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: document.getElementById('setup-username').value.trim(),
            password: document.getElementById('setup-password').value,
            backupAcknowledged: document.getElementById('setup-backup-ack').checked,
            locale: window.veilLocale
          })
        });
        const text = await response.text();
        if (!response.ok) {
          result.className = 'error';
          result.textContent = text || ('HTTP ' + response.status);
          return;
        }
        completed = true;
        result.textContent = veilT('setup.done');
        window.setTimeout(() => window.location.reload(), 500);
      } catch (error) {
        result.className = 'error';
        result.textContent = veilT('setup.failed', { error: String(error) });
      } finally {
        if (!completed) {
          setupInFlight = false;
          button.disabled = false;
        }
      }
    });
  </script>
</body>
</html>`
