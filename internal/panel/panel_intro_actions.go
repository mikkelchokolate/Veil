package panel

const panelIntroActionsPlaceholder = "__VEIL_PANEL_INTRO_ACTIONS__"

func panelIntroActionsJS() string {
	return `    const tokenInput = document.getElementById('api-token');
    tokenInput.value = localStorage.getItem('veil_api_token') || '';
    tokenInput.addEventListener('input', () => {
      localStorage.setItem('veil_api_token', tokenInput.value);
    });

    // Toggle API Token Visibility
    const toggleBtn = document.getElementById('toggle-api-token-visibility');
    if (toggleBtn) {
      toggleBtn.addEventListener('click', () => {
        const isPassword = tokenInput.getAttribute('type') === 'password';
        tokenInput.setAttribute('type', isPassword ? 'text' : 'password');
        toggleBtn.innerHTML = isPassword 
          ? '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.542-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l18 18"/></svg>'
          : '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>';
      });
    }

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
    });

    document.getElementById('update-version').addEventListener('click', async () => {
      const btn = document.getElementById('update-version');
      const output = document.getElementById('version-output');
      btn.disabled = true;
      output.textContent = 'Starting self-update... Fetching latest release, verifying checksums and installing binary. Please wait, this will take a moment...';
      
      try {
        const response = await fetch('/api/version/update', {
          method: 'POST',
          headers: requestHeaders({ 'Content-Type': 'application/json' })
        });
        const text = await response.text();
        let data = null;
        try { data = JSON.parse(text); } catch (_) {}
        
        if (!response.ok) {
          btn.disabled = false;
          if (data && data.log) {
            output.textContent = "UPDATE FAILED:\n" + data.log + "\n\nError: " + (data.message || 'unknown error');
          } else {
            output.textContent = "UPDATE FAILED:\n" + text;
          }
          return;
        }
        
        output.textContent = (data && data.log ? data.log + "\n\n" : "") + "Update staged successfully! Restarting panel service to apply...";
        
        setTimeout(() => {
          let attempts = 0;
          const maxAttempts = 20;
          const pollInterval = setInterval(async () => {
            attempts++;
            output.textContent = "Staging completed. Waiting for service restart and health check... (Attempt " + attempts + "/" + maxAttempts + ")";
            try {
              const checkResp = await fetch('/api/version', { headers: authHeaders() });
              if (checkResp.ok) {
                const checkData = await checkResp.json();
                clearInterval(pollInterval);
                btn.disabled = false;
                output.textContent = "SERVICE BACK ONLINE!\n\n" + JSON.stringify(checkData, null, 2);
              }
            } catch (_) {
            }
            if (attempts >= maxAttempts) {
              clearInterval(pollInterval);
              btn.disabled = false;
              output.textContent = "Restart is taking longer than expected. Please refresh the page manually in a few seconds to check status.";
            }
          }, 2000);
        }, 3000);
        
      } catch (err) {
        btn.disabled = false;
        output.textContent = "Request error: " + String(err);
      }
    });`
}
