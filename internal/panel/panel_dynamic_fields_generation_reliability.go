package panel

// panelDynamicFieldsGenerationReliabilityJS replaces the optimistic protocol
// field generator with a generation-safe implementation. Provider changes and
// repeated clicks must not allow a stale room response to overwrite newer form
// state, and request failures must be visible in the inbound validation UI.
func panelDynamicFieldsGenerationReliabilityJS() string {
	return `
    const protocolFieldGenerationControllers = new Map();
    const protocolFieldGenerationSequences = new Map();

    function restoreProtocolGenerateButton(button, actionField) {
      if (!button) return;
      if (!actionField) {
        button.disabled = isViewerRole();
        return;
      }
      const authElement = document.getElementById(protocolFieldElementId(actionField));
      const selectedOption = authElement && authElement.selectedOptions ? authElement.selectedOptions[0] : null;
      const supportsGeneration = Boolean(selectedOption && selectedOption.dataset.autoroom === 'true');
      button.disabled = isViewerRole() || !supportsGeneration;
    }

    const baseRenderDynamicProtocolFields = window.veilRenderDynamicProtocolFields;
    window.veilRenderDynamicProtocolFields = function(container, protocol, values) {
      const result = baseRenderDynamicProtocolFields(container, protocol, values);
      container.querySelectorAll('button[id$="-generate"]').forEach((button) => {
        button.dataset.adminOnly = 'true';
      });
      applyViewerRoleGuard();
      return result;
    };

    window.veilGenerateProtocolField = async function(protocol, key, action, actionField) {
      const element = document.getElementById(protocolFieldElementId(key));
      const button = document.getElementById(protocolFieldElementId(key) + '-generate');
      if (!element || !button) return false;

      const requestKey = String(protocol || '') + ':' + String(key || '');
      const previousController = protocolFieldGenerationControllers.get(requestKey);
      if (previousController) previousController.abort();
      const controller = new AbortController();
      protocolFieldGenerationControllers.set(requestKey, controller);
      const sequence = (protocolFieldGenerationSequences.get(requestKey) || 0) + 1;
      protocolFieldGenerationSequences.set(requestKey, sequence);
      button.disabled = true;

      try {
        if (action === 'password') {
          element.value = randomPassword();
        } else if (action === 'room') {
          const authElement = actionField ? document.getElementById(protocolFieldElementId(actionField)) : null;
          const provider = authElement ? authElement.value : '';
          const response = await fetch('/api/' + encodeURIComponent(protocol) + '/room', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ provider }),
            signal: controller.signal
          });
          const text = await response.text();
          if (sequence !== protocolFieldGenerationSequences.get(requestKey)) return false;
          if (authElement && authElement.value !== provider) return false;
          if (!response.ok) throw new Error(formatAPIError(text, response.status));
          const data = text ? JSON.parse(text) : {};
          if (!data || typeof data.roomID !== 'string' || !data.roomID) {
            throw new Error('Room generation response is missing roomID.');
          }
          element.value = data.roomID;
        } else {
          return false;
        }
        scheduleInboundValidation();
        return true;
      } catch (error) {
        if (error && error.name === 'AbortError') return false;
        if (sequence === protocolFieldGenerationSequences.get(requestKey)) {
          veilShowInboundLocalError(
            'Could not generate ' + String(key || 'protocol field') + ': ' + String(error && error.message ? error.message : error),
            'protocolFields.' + String(key || '')
          );
        }
        return false;
      } finally {
        if (protocolFieldGenerationControllers.get(requestKey) === controller) {
          protocolFieldGenerationControllers.delete(requestKey);
          restoreProtocolGenerateButton(button, actionField);
        }
      }
    };
`
}
