package panel

const panelIntroActionsPlaceholder = "__VEIL_PANEL_INTRO_ACTIONS__"

func panelIntroActionsJS() string {
	return `    const tokenInput = document.getElementById('api-token');
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

    document.getElementById('profile-preview-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const domain = document.getElementById('profile-domain').value.trim();
      const email = document.getElementById('profile-email').value.trim();
      const panelAccess = document.getElementById('profile-panel-access').value;
      if (panelAccess === 'caddy' && (!domain || !email)) {
        document.getElementById('profile-preview-output').textContent = 'Domain and email are required for Caddy Panel access';
        return;
      }
      await loadJSON('/api/profiles/ru-recommended/preview', 'profile-preview-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain, email, panelAccess })
      });
    });

    document.getElementById('load-version').addEventListener('click', async () => {
      await loadJSON('/api/version', 'version-output');
    });`
}
