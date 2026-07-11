package panel

// panelDataLoadRequestReliabilityJS serializes raw data-load controls that
// share one output. Routing rules and presets both write routing-output, so a
// slower earlier request must not overwrite the response selected later.
func panelDataLoadRequestReliabilityJS() string {
	return `
    const dataLoadControlSelectorByOutput = Object.freeze({
      'routing-output': '[data-load][data-output="routing-output"]'
    });
    const baseLoadJSONForDataLoadControls = loadJSON;
    loadJSON = async function(path, outputId, options) {
      const selector = dataLoadControlSelectorByOutput[String(outputId || '')];
      if (!selector) return baseLoadJSONForDataLoadControls(path, outputId, options);

      const controls = Array.from(document.querySelectorAll(selector));
      if (controls.some((control) => control.dataset.dataLoadInFlight === 'true')) return null;
      const previousDisabled = controls.map((control) => control.disabled);
      controls.forEach((control) => {
        control.dataset.dataLoadInFlight = 'true';
        control.disabled = true;
      });
      try {
        return await baseLoadJSONForDataLoadControls(path, outputId, options);
      } finally {
        const authResetPending = typeof veilAuthenticationReloadScheduled !== 'undefined'
          && veilAuthenticationReloadScheduled;
        controls.forEach((control, index) => {
          delete control.dataset.dataLoadInFlight;
          if (!authResetPending) control.disabled = previousDisabled[index];
        });
      }
    };
`
}
