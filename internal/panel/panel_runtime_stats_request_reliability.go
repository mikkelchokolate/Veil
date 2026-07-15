package panel

// panelRuntimeStatsRequestReliabilityJS prevents repeated clicks on one manual
// runtime-stat control from starting overlapping requests. The first request
// remains authoritative and the control is restored after success or failure.
func panelRuntimeStatsRequestReliabilityJS() string {
	return `
    const runtimeStatsButtonByOutput = Object.freeze({
      'system-stats-output': 'load-system-stats',
      'network-stats-output': 'load-network-stats',
      'connections-stats-output': 'load-connections-stats',
      'processes-stats-output': 'load-processes-stats',
      'disk-stats-output': 'load-disk-stats'
    });
    const baseLoadJSONForRuntimeStats = loadJSON;
    loadJSON = async function(path, outputId, options) {
      const buttonID = runtimeStatsButtonByOutput[String(outputId || '')];
      if (!buttonID) return baseLoadJSONForRuntimeStats(path, outputId, options);
      const button = document.getElementById(buttonID);
      if (!button) return baseLoadJSONForRuntimeStats(path, outputId, options);
      if (button.dataset.runtimeStatsInFlight === 'true') return null;

      const wasDisabled = button.disabled;
      button.dataset.runtimeStatsInFlight = 'true';
      button.disabled = true;
      try {
        return await baseLoadJSONForRuntimeStats(path, outputId, options);
      } finally {
        delete button.dataset.runtimeStatsInFlight;
        const authResetPending = typeof veilAuthenticationReloadScheduled !== 'undefined'
          && veilAuthenticationReloadScheduled;
        if (!authResetPending) button.disabled = wasDisabled;
      }
    };
`
}
