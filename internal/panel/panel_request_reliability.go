package panel

// panelRequestReliabilityJS normalizes the shared JSON helper so successful
// 204/empty responses are distinguishable from request failures. Several Panel
// delete actions need that distinction before closing their editor or reloading.
// Request options and headers are cloned so a reused caller object cannot retain
// stale auth/CSRF headers or be mutated as a side effect of one request.
func panelRequestReliabilityJS() string {
	return `    loadJSON = async function(path, outputId, options) {
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
        if (!text) {
          if (output) output.textContent = veilT('common.ok');
          return true;
        }
        const parsed = JSON.parse(text);
        if (output) output.textContent = JSON.stringify(parsed, null, 2);
        return parsed;
      } catch (err) {
        if (output) output.textContent = String(err);
        return null;
      }
    };
`
}
