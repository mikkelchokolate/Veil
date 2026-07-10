package panel

// panelInboundReliabilityJS patches the inbound modal after the base actions are
// declared. Keeping this stateful coordination separate avoids coupling the
// generic client-profile controls to the transport and persistence modules.
func panelInboundReliabilityJS() string {
	return `    let veilEditingInboundName = '';

    function veilShowInboundLocalError(message, field) {
      inboundValidationValid = false;
      clearInboundValidation();
      const summary = document.getElementById('inbound-validation-summary');
      if (summary) {
        summary.className = 'validation-summary validation-error';
        summary.textContent = String(message || 'Inbound validation failed.');
      }
      const control = field ? inboundFieldControl(field) : null;
      if (control) {
        control.setAttribute('aria-invalid', 'true');
        const describedBy = control.getAttribute('aria-describedby');
        const fieldMessage = describedBy ? document.getElementById(describedBy) : null;
        if (fieldMessage) {
          fieldMessage.hidden = false;
          fieldMessage.textContent = String(message || 'Invalid value.');
        }
      }
      const saveButton = document.getElementById('save-inbound');
      if (saveButton) saveButton.disabled = true;
    }

    const veilBaseEnsureProtocolSchemas = ensureProtocolSchemas;
    ensureProtocolSchemas = function() {
      return veilBaseEnsureProtocolSchemas().catch((error) => {
        // A transient request failure must not poison every later modal open.
        window.protocolSchemaPromise = null;
        throw error;
      });
    };

    const veilBaseOpenAddInboundModal = window.openAddInboundModal;
    window.openAddInboundModal = function() {
      veilEditingInboundName = '';
      return veilBaseOpenAddInboundModal.apply(this, arguments);
    };

    const veilBaseOpenEditInboundModal = window.openEditInboundModal;
    window.openEditInboundModal = function(name) {
      veilEditingInboundName = String(name || '');
      return veilBaseOpenEditInboundModal.apply(this, arguments);
    };

    const veilBaseCloseInboundModal = window.closeInboundModal;
    window.closeInboundModal = function() {
      veilEditingInboundName = '';
      return veilBaseCloseInboundModal.apply(this, arguments);
    };

    const veilBaseValidateInboundCandidate = validateInboundCandidate;
    validateInboundCandidate = async function() {
      const form = document.getElementById('inbound-form');
      if (form && !form.checkValidity()) {
        form.reportValidity();
        const name = document.getElementById('inbound-name').value.trim();
        const field = name ? 'port' : 'name';
        veilShowInboundLocalError('Name and a port between 1 and 65535 are required.', field);
        return false;
      }
      const name = document.getElementById('inbound-name').value.trim();
      if (!veilEditingInboundName && Array.isArray(window.cachedInbounds) && window.cachedInbounds.some((inbound) => inbound.name === name)) {
        veilShowInboundLocalError('An inbound with this name already exists.', 'name');
        return false;
      }
      return veilBaseValidateInboundCandidate();
    };

    saveInbound = async function(event) {
      event.preventDefault();
      if (!await validateInboundCandidate()) return;

      let payload;
      try {
        payload = buildInboundCandidate();
      } catch (err) {
        veilShowInboundLocalError('Client profiles must be valid JSON: ' + String(err));
        return;
      }

      const editingName = veilEditingInboundName;
      const saved = await loadJSON(editingName ? '/api/inbounds/' + encodeURIComponent(editingName) : '/api/inbounds', 'inbounds-output', {
        method: editingName ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!saved) return;

      closeInboundModal();
      loadInboundsIntoOutput();
    };

    deleteInbound = async function() {
      const name = veilEditingInboundName || document.getElementById('inbound-name').value.trim();
      if (!name || !confirm(veilT('confirm.deleteInbound', { name }))) return;
      const deleted = await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
      if (!deleted) return;
      closeInboundModal();
      loadInboundsIntoOutput();
    };

    window.renderDynamicProtocolFields = function(inbound) {
      const container = document.getElementById('inbound-protocol-fields');
      if (!container) return Promise.resolve(false);
      const protocol = document.getElementById('inbound-protocol').value;
      const values = inbound && inbound.protocolFields ? inbound.protocolFields : {};
      return ensureProtocolSchemas().then(() => {
        veilRenderDynamicProtocolFields(container, protocol, values);
        scheduleInboundValidation();
        return true;
      }).catch((error) => {
        container.innerHTML = '';
        veilShowInboundLocalError('Could not load protocol fields: ' + String(error && error.message ? error.message : error), 'protocol');
        return false;
      });
    };
`
}
