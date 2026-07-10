package panel

// panelInboundReliabilityJS patches the inbound modal after the base actions are
// declared. Keeping this stateful coordination separate avoids coupling the
// generic client-profile controls to the transport and persistence modules.
func panelInboundReliabilityJS() string {
	return `    let veilEditingInboundName = '';
    let inboundLoadGeneration = 0;
    let inboundLoadController = null;
    let inboundMutationInFlight = false;

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

    function setInboundMutationControlsDisabled(disabled) {
      const busy = Boolean(disabled);
      const viewer = isViewerRole();
      const saveButton = document.getElementById('save-inbound');
      if (saveButton) saveButton.disabled = busy || !inboundValidationValid || viewer;
      ['add-inbound-btn', 'delete-inbound'].forEach((id) => {
        const button = document.getElementById(id);
        if (button) button.disabled = busy || viewer;
      });
      const loadButton = document.getElementById('load-inbounds');
      if (loadButton) loadButton.disabled = busy;
      document.querySelectorAll('#inbounds-table input[data-admin-only="true"], [data-inbound-action="edit"], [data-inbound-action="delete"]').forEach((control) => {
        control.disabled = busy || viewer;
      });
    }

    function cancelInboundLoad() {
      inboundLoadGeneration += 1;
      if (inboundLoadController) {
        inboundLoadController.abort();
        inboundLoadController = null;
      }
    }

    async function withInboundMutation(action) {
      if (inboundMutationInFlight) return null;
      inboundMutationInFlight = true;
      cancelInboundLoad();
      setInboundMutationControlsDisabled(true);
      try {
        return await action();
      } finally {
        inboundMutationInFlight = false;
        setInboundMutationControlsDisabled(false);
        applyViewerRoleGuard();
      }
    }

    loadInboundsIntoOutput = async function() {
      if (inboundMutationInFlight) return null;
      const generation = ++inboundLoadGeneration;
      if (inboundLoadController) inboundLoadController.abort();
      const controller = new AbortController();
      inboundLoadController = controller;
      const output = document.getElementById('inbounds-output');
      const loadButton = document.getElementById('load-inbounds');
      if (loadButton) loadButton.disabled = true;
      if (output) output.textContent = veilT('status.loadingPath', { path: '/api/inbounds' });
      try {
        const response = await fetch('/api/inbounds', {
          headers: authHeaders(),
          signal: controller.signal
        });
        const text = await response.text();
        if (generation !== inboundLoadGeneration || controller.signal.aborted) return null;
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const inbounds = text ? JSON.parse(text) : [];
        if (!Array.isArray(inbounds)) throw new Error('Invalid inbounds response.');
        if (output) output.textContent = JSON.stringify(inbounds, null, 2);
        window.cachedInbounds = inbounds;
        const tbody = document.getElementById('inbounds-tbody');
        if (tbody) {
          tbody.textContent = '';
          if (inbounds.length === 0) {
            appendInboundEmptyState(tbody);
          } else {
            inbounds.forEach((inbound) => tbody.appendChild(createInboundRow(inbound)));
          }
        }
        applyViewerRoleGuard();
        return inbounds;
      } catch (error) {
        if (error && error.name === 'AbortError') return null;
        if (generation !== inboundLoadGeneration) return null;
        if (output) {
          output.textContent = veilT('status.requestFailed', { error: String(error && error.message ? error.message : error) });
        }
        return null;
      } finally {
        if (inboundLoadController === controller) inboundLoadController = null;
        if (loadButton && generation === inboundLoadGeneration) loadButton.disabled = inboundMutationInFlight;
      }
    };

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
      if (event) event.preventDefault();
      const saved = await withInboundMutation(async () => {
        if (!await validateInboundCandidate()) return null;
        setInboundMutationControlsDisabled(true);
        let payload;
        try {
          payload = buildInboundCandidate();
        } catch (err) {
          veilShowInboundLocalError('Client profiles must be valid JSON: ' + String(err));
          return null;
        }
        const editingName = veilEditingInboundName;
        return loadJSON(editingName ? '/api/inbounds/' + encodeURIComponent(editingName) : '/api/inbounds', 'inbounds-output', {
          method: editingName ? 'PUT' : 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
      });
      if (!saved) return null;
      closeInboundModal();
      await loadInboundsIntoOutput();
      return saved;
    };

    deleteInbound = async function() {
      const name = veilEditingInboundName || document.getElementById('inbound-name').value.trim();
      if (!name || !confirm(veilT('confirm.deleteInbound', { name }))) return null;
      const deleted = await withInboundMutation(() => loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' }));
      if (!deleted) return null;
      closeInboundModal();
      await loadInboundsIntoOutput();
      return deleted;
    };

    window.toggleInboundActive = async function(name, checked, control) {
      const updated = await withInboundMutation(async () => {
        const inbound = Array.isArray(window.cachedInbounds) ? window.cachedInbounds.find((item) => item.name === name) : null;
        if (!inbound) return null;
        const payload = Object.assign({}, inbound, { enabled: checked });
        return loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
      });
      if (!updated) {
        if (control) {
          control.checked = !checked;
          control.disabled = isViewerRole();
        }
        return null;
      }
      await loadInboundsIntoOutput();
      return updated;
    };

    window.directDeleteInbound = async function(name) {
      if (!confirm(veilT('confirm.deleteInbound', { name }))) return null;
      const deleted = await withInboundMutation(() => loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' }));
      if (!deleted) return null;
      await loadInboundsIntoOutput();
      return deleted;
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
        container.textContent = '';
        veilShowInboundLocalError('Could not load protocol fields: ' + String(error && error.message ? error.message : error), 'protocol');
        return false;
      });
    };
`
}
