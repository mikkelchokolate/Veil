package panel

// panelRequestReliabilityJS normalizes the shared JSON helper so successful
// 204/empty responses are distinguishable from request failures. Several Panel
// delete actions need that distinction before closing their editor or reloading.
// Request options and headers are cloned so a reused caller object cannot retain
// stale auth/CSRF headers or be mutated as a side effect of one request.
func panelRequestReliabilityJS() string {
	return `    function isPanelConfigurationMutation(path, options) {
      const method = String(options && options.method || 'GET').toUpperCase();
      if (method === 'GET' || method === 'HEAD') return false;
      const endpoint = String(path || '').split('?')[0];
      return endpoint === '/api/settings'
        || endpoint === '/api/warp'
        || endpoint === '/api/inbounds'
        || endpoint.startsWith('/api/inbounds/')
        || endpoint === '/api/routing/rules'
        || endpoint.startsWith('/api/routing/rules/')
        || endpoint.startsWith('/api/routing/presets/');
    }

    function notifyPanelConfigurationChanged(path) {
      document.dispatchEvent(new CustomEvent('veil:configuration-changed', {
        detail: { path: String(path || '') }
      }));
    }

    loadJSON = async function(path, outputId, options) {
      const output = document.getElementById(outputId);
      if (output) output.textContent = veilT('status.loadingPath', { path });
      const requestOptions = Object.assign({}, options || {});
      requestOptions.headers = requestHeaders(Object.assign({}, requestOptions.headers || {}));
      try {
        const response = await fetch(path, requestOptions);
        const text = await response.text();
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const configurationChanged = isPanelConfigurationMutation(path, requestOptions);
        if (!text) {
          if (output) output.textContent = veilT('common.ok');
          if (configurationChanged) notifyPanelConfigurationChanged(path);
          return true;
        }
        const parsed = JSON.parse(text);
        if (output) output.textContent = JSON.stringify(parsed, null, 2);
        if (configurationChanged) notifyPanelConfigurationChanged(path);
        return parsed;
      } catch (err) {
        if (output) output.textContent = String(err);
        return null;
      }
    };
`
}
