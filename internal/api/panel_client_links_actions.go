package api

const panelClientLinksActionsPlaceholder = "__VEIL_PANEL_CLIENT_LINKS_ACTIONS__"

func panelClientLinksActionsJS() string {
	return `    async function loadClientLinks() {
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

    async function downloadMieruConfigs() {
      const output = document.getElementById('client-links-output');
      output.textContent = 'Loading Mieru client configs...';
      try {
        const response = await fetch('/api/client-links', { headers: requestHeaders() });
        const body = await response.json();
        if (!response.ok) {
          output.textContent = JSON.stringify(body, null, 2);
          return;
        }
        const configs = (body.links || []).filter(link => link.protocol === 'mieru' && link.config).map(link => ({ name: link.name, config: JSON.parse(link.config) }));
        if (!configs.length) {
          output.textContent = 'No Mieru client configs available';
          return;
        }
        const blob = new Blob([JSON.stringify(configs, null, 2) + '\n'], { type: 'application/json;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = 'mieru-client-configs.json';
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        output.textContent = 'Downloaded mieru-client-configs.json';
      } catch (err) {
        output.textContent = 'Mieru config download failed: ' + String(err);
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
    }`
}
