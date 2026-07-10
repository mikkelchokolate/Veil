package panel

// panelRequestReliabilityJS normalizes the shared JSON helper so successful
// 204/empty responses are distinguishable from request failures. Several Panel
// delete actions need that distinction before closing their editor or reloading.
func panelRequestReliabilityJS() string {
	return `    loadJSON = async function(path, outputId, options) {
      const output = document.getElementById(outputId);
      if (output) output.textContent = veilT('status.loadingPath', { path });
      const requestOptions = options || {};
      requestOptions.headers = requestHeaders(requestOptions.headers || {});
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
