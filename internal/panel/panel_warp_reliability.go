package panel

import "strings"

func panelWarpReliableActionsJS() string {
	actions := panelWarpActionsJS()
	actions = strings.Replace(actions, `    // Auto-load WARP config on page mount
    setTimeout(loadWarpIntoForm, 150);`, "", 1)
	return actions + panelWarpLoadReliabilityJS()
}

func panelWarpLoadReliabilityJS() string {
	return `
    let warpLoadGeneration = 0;
    let warpLoadController = null;

    function cancelWarpLoad() {
      warpLoadGeneration += 1;
      if (warpLoadController) {
        warpLoadController.abort();
        warpLoadController = null;
      }
      const loadButton = document.getElementById('load-warp-config');
      if (loadButton) loadButton.disabled = warpCommitInFlight;
    }

    loadWarpIntoForm = async function() {
      if (warpCommitInFlight) return null;
      const generation = ++warpLoadGeneration;
      if (warpLoadController) warpLoadController.abort();
      const controller = new AbortController();
      warpLoadController = controller;
      const output = document.getElementById('warp-output');
      const loadButton = document.getElementById('load-warp-config');
      if (loadButton) loadButton.disabled = true;
      if (output) output.textContent = veilT('status.loadingPath', { path: '/api/warp' });
      try {
        const response = await fetch('/api/warp', {
          headers: authHeaders(),
          signal: controller.signal
        });
        const text = await response.text();
        if (generation !== warpLoadGeneration || controller.signal.aborted) return null;
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const data = text ? JSON.parse(text) : {};
        if (!data || typeof data !== 'object' || Array.isArray(data)) {
          throw new Error('Invalid WARP response.');
        }
        if (output) output.textContent = JSON.stringify(data, null, 2);
        window.cachedWarp = data;
        fillWarpForm(data);
        return data;
      } catch (error) {
        if (error && error.name === 'AbortError') return null;
        if (generation !== warpLoadGeneration) return null;
        if (output) {
          output.textContent = veilT('status.requestFailed', { error: String(error && error.message ? error.message : error) });
        }
        return null;
      } finally {
        if (warpLoadController === controller) warpLoadController = null;
        if (loadButton && generation === warpLoadGeneration) loadButton.disabled = warpCommitInFlight;
      }
    };

    const veilBaseCommitWarp = commitWarp;
    commitWarp = async function(enabled) {
      cancelWarpLoad();
      const loadButton = document.getElementById('load-warp-config');
      if (loadButton) loadButton.disabled = true;
      try {
        return await veilBaseCommitWarp(enabled);
      } finally {
        if (loadButton) loadButton.disabled = warpCommitInFlight;
      }
    };

    const warpFormForLoadCancellation = document.getElementById('warp-form');
    if (warpFormForLoadCancellation) {
      warpFormForLoadCancellation.addEventListener('input', cancelWarpLoad);
    }
`
}
