package panel

const panelInboundActionsPlaceholder = "__VEIL_PANEL_INBOUND_ACTIONS__"

func panelInboundActionsJS() string {
	return panelInboundProtocolTransportRulesJS() + `    // Global cache and modal handlers.
    window.cachedInbounds = [];
    window.cachedSettings = window.cachedSettings || null;
    window.cachedWarp = window.cachedWarp || null;
    window.inboundEditorMode = 'add';
    window.inboundEditorOriginalName = '';

    const inboundValidationDebounceMs = 300;
    let inboundValidationTimer = null;
    let inboundValidationController = null;
    let inboundValidationSequence = 0;
    let inboundValidationValid = false;

    function clearInboundValidation() {
      document.querySelectorAll('#inbound-form [aria-invalid]').forEach((control) => {
        control.setAttribute('aria-invalid', 'false');
      });
      document.querySelectorAll('#inbound-form .field-validation').forEach((message) => {
        message.hidden = true;
        message.textContent = '';
      });
      const summary = document.getElementById('inbound-validation-summary');
      if (summary) {
        summary.className = 'validation-summary';
        summary.textContent = veilT('inbounds.validationPending');
      }
    }

    function inboundFieldControl(field) {
      const normalized = String(field || '').replace(/^inbounds\[[0-9]+\]\./, '');
      const ids = {
        name: 'inbound-name',
        protocol: 'inbound-protocol',
        transport: 'inbound-transport',
        port: 'inbound-port',
        password: 'inbound-password'
      };
      return document.getElementById(ids[normalized] || '');
    }

    function renderInboundValidation(response) {
      clearInboundValidation();
      const issues = response && Array.isArray(response.issues) ? response.issues : [];
      const summary = document.getElementById('inbound-validation-summary');
      const errors = issues.filter((issue) => issue.severity === 'error');
      issues.forEach((issue) => {
        const control = inboundFieldControl(issue.field);
        if (!control) {
          return;
        }
        control.setAttribute('aria-invalid', issue.severity === 'error' ? 'true' : 'false');
        const describedBy = control.getAttribute('aria-describedby');
        const message = describedBy ? document.getElementById(describedBy) : null;
        if (message) {
          message.hidden = false;
          message.textContent = veilValidationIssueText(issue);
        }
      });
      if (summary) {
        summary.className = errors.length > 0 ? 'validation-summary validation-error' : 'validation-summary validation-ok';
        if (issues.length === 0) {
          summary.textContent = veilT('inbounds.validationReady');
        } else {
          summary.textContent = issues.map(veilValidationIssueText).join(' ');
        }
      }
      inboundValidationValid = !!(response && response.valid);
      const saveButton = document.getElementById('save-inbound');
      if (saveButton) {
        saveButton.disabled = !inboundValidationValid || isViewerRole();
      }
      return inboundValidationValid;
    }

    function buildInboundCandidate() {
      const name = document.getElementById('inbound-name').value.trim();
      const protocol = document.getElementById('inbound-protocol').value;
      const payload = {
        name: name,
        protocol: protocol,
        transport: document.getElementById('inbound-transport').value,
        port: numberOrZero('inbound-port'),
        enabled: document.getElementById('inbound-enabled').checked
      };
      const profilesRaw = document.getElementById('inbound-profiles').value.trim();
      if (profilesRaw) {
        payload.profiles = JSON.parse(profilesRaw);
      }
      const password = document.getElementById('inbound-password').value.trim();
      if (password) {
        payload.password = password;
      }
      payload.protocolFields = veilCollectProtocolFields(protocol);
      return payload;
    }

    async function ensureInboundValidationContext() {
      if (window.cachedSettings && window.cachedWarp) {
        return true;
      }
      const responses = await Promise.all([
        fetch('/api/settings', { headers: authHeaders() }),
        fetch('/api/warp', { headers: authHeaders() })
      ]);
      if (!responses[0].ok || !responses[1].ok) {
        return false;
      }
      window.cachedSettings = await responses[0].json();
      window.cachedWarp = await responses[1].json();
      return true;
    }

    async function validateInboundCandidate() {
      const sequence = ++inboundValidationSequence;
      if (inboundValidationController) {
        inboundValidationController.abort();
      }
      inboundValidationController = new AbortController();
      const saveButton = document.getElementById('save-inbound');
      if (saveButton) {
        saveButton.disabled = true;
      }
      try {
        if (!await ensureInboundValidationContext()) {
          throw new Error('Could not load settings required for validation.');
        }
        const candidate = buildInboundCandidate();
        const inbounds = Array.isArray(window.cachedInbounds) ? window.cachedInbounds.map((item) => Object.assign({}, item)) : [];
        if (window.inboundEditorMode === 'edit') {
          const existingIndex = inbounds.findIndex((item) => item.name === window.inboundEditorOriginalName);
          if (existingIndex >= 0) {
            inbounds[existingIndex] = candidate;
          } else {
            inbounds.push(candidate);
          }
        } else {
          // Keep an existing inbound with the same name in the candidate list so
          // validation and the eventual POST cannot silently turn Add into Edit.
          inbounds.push(candidate);
        }
        const response = await fetch('/api/validation', {
          method: 'POST',
          headers: requestHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify({ settings: window.cachedSettings, inbounds: inbounds, warp: window.cachedWarp }),
          signal: inboundValidationController.signal
        });
        const data = await response.json();
        if (sequence !== inboundValidationSequence) {
          return false;
        }
        if (!response.ok) {
          throw new Error(data && data.error && data.error.message ? data.error.message : 'Validation request failed.');
        }
        return renderInboundValidation(data);
      } catch (error) {
        if (error && error.name === 'AbortError') {
          return false;
        }
        if (sequence !== inboundValidationSequence) {
          return false;
        }
        inboundValidationValid = false;
        clearInboundValidation();
        const summary = document.getElementById('inbound-validation-summary');
        if (summary) {
          summary.className = 'validation-summary validation-error';
          summary.textContent = String(error && error.message ? error.message : error);
        }
        if (saveButton) {
          saveButton.disabled = true;
        }
        return false;
      }
    }

    function scheduleInboundValidation() {
      inboundValidationValid = false;
      const saveButton = document.getElementById('save-inbound');
      if (saveButton) {
        saveButton.disabled = true;
      }
      const summary = document.getElementById('inbound-validation-summary');
      if (summary) {
        summary.className = 'validation-summary';
        summary.textContent = veilT('inbounds.validationChecking');
      }
      clearTimeout(inboundValidationTimer);
      inboundValidationTimer = setTimeout(validateInboundCandidate, inboundValidationDebounceMs);
    }

    window.openAddInboundModal = function() {
      window.inboundEditorMode = 'add';
      window.inboundEditorOriginalName = '';
      document.getElementById('inbound-modal-title').innerText = veilT('modal.addInbound');
      document.getElementById('inbound-name').value = '';
      document.getElementById('inbound-name').readOnly = false;
      const protocolSelect = document.getElementById('inbound-protocol');
      protocolSelect.value = protocolSelect.options[0] ? protocolSelect.options[0].value : '';
      document.getElementById('inbound-transport').value = 'tcp';
      document.getElementById('inbound-port').value = '';
      document.getElementById('inbound-password').value = '';
      document.getElementById('inbound-enabled').checked = true;
      document.getElementById('inbound-profiles').value = '';

      document.getElementById('client-profile-name').value = '';
      document.getElementById('client-profile-username').value = '';
      document.getElementById('client-profile-password').value = '';

      document.getElementById('delete-inbound').style.display = 'none';
      syncInboundTransportOptions();
      renderDynamicProtocolFields(null);

      openVeilDialog(document.getElementById('inbound-modal-overlay'));
      clearInboundValidation();
      scheduleInboundValidation();
    };

    window.closeInboundModal = function() {
      closeVeilDialog(document.getElementById('inbound-modal-overlay'));
      clearTimeout(inboundValidationTimer);
      if (inboundValidationController) {
        inboundValidationController.abort();
      }
    };

    window.openEditInboundModal = function(name) {
      const inbound = window.cachedInbounds.find(i => i.name === name);
      if (!inbound) return;

      window.inboundEditorMode = 'edit';
      window.inboundEditorOriginalName = inbound.name;
      document.getElementById('inbound-modal-title').innerText = veilT('inbounds.editTitle', { name });
      document.getElementById('inbound-name').value = inbound.name;
      document.getElementById('inbound-name').readOnly = true;
      document.getElementById('inbound-protocol').value = inbound.protocol;
      syncInboundTransportOptions();
      document.getElementById('inbound-transport').value = inbound.transport;
      document.getElementById('inbound-port').value = inbound.port || '';
      document.getElementById('inbound-password').value = inbound.password || '';
      document.getElementById('inbound-enabled').checked = !!inbound.enabled;
      document.getElementById('inbound-profiles').value = inbound.profiles ? JSON.stringify(inbound.profiles, null, 2) : '[]';

      document.getElementById('client-profile-name').value = '';
      document.getElementById('client-profile-username').value = '';
      document.getElementById('client-profile-password').value = '';

      document.getElementById('delete-inbound').style.display = 'block';
      renderDynamicProtocolFields(inbound);

      openVeilDialog(document.getElementById('inbound-modal-overlay'));
      clearInboundValidation();
      scheduleInboundValidation();
    };

    window.toggleInboundActive = async function(name, checked, control) {
      const inbound = window.cachedInbounds.find(i => i.name === name);
      if (!inbound) return;
      if (control) control.disabled = true;
      const payload = Object.assign({}, inbound, { enabled: checked });
      const updated = await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (updated === null) {
        if (control) {
          control.checked = !checked;
          control.disabled = isViewerRole();
        }
        return;
      }
      await loadInboundsIntoOutput();
    };

    window.directDeleteInbound = async function(name) {
      if (confirm(veilT('confirm.deleteInbound', { name }))) {
        const deleted = await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
        if (deleted !== null) {
          await loadInboundsIntoOutput();
        }
      }
    };

    function appendInboundEmptyState(tbody) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.style.textAlign = 'center';
      cell.style.color = 'var(--text-muted)';
      cell.style.padding = '24px';
      cell.textContent = 'No inbounds configured. Click Add Inbound to start.';
      row.appendChild(cell);
      tbody.appendChild(row);
    }

    function inboundBadgeStyle(protocol) {
      if (protocol === 'hysteria2') {
        return 'background: rgba(139, 92, 246, 0.15); color: #a78bfa; border: 1px solid rgba(139, 92, 246, 0.3);';
      }
      if (protocol === 'mieru') {
        return 'background: rgba(59, 130, 246, 0.15); color: #60a5fa; border: 1px solid rgba(59, 130, 246, 0.3);';
      }
      if (protocol === 'olcrtc') {
        return 'background: rgba(16, 185, 129, 0.15); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.3);';
      }
      return 'background: rgba(79, 70, 229, 0.15); color: #818cf8; border: 1px solid rgba(79, 70, 229, 0.3);';
    }

    function createInboundActionButton(label, action, inbound, className) {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = label;
      button.dataset.inboundAction = action;
      button.dataset.inboundName = String(inbound.name || '');
      button.dataset.inboundProtocol = String(inbound.protocol || '');
      if (className) button.className = className;
      if (action === 'edit' || action === 'delete') {
        button.dataset.adminOnly = 'true';
      }
      if (action === 'delete') {
        button.style.color = 'var(--accent-danger)';
      }
      return button;
    }

    function createInboundRow(inbound) {
      const row = document.createElement('tr');

      const nameCell = document.createElement('td');
      const name = document.createElement('strong');
      name.style.color = '#fff';
      name.style.fontSize = '0.95rem';
      name.textContent = String(inbound.name || '');
      nameCell.appendChild(name);
      row.appendChild(nameCell);

      const protocolCell = document.createElement('td');
      const protocolBadge = document.createElement('span');
      protocolBadge.className = 'badge';
      protocolBadge.style.cssText = inboundBadgeStyle(inbound.protocol);
      protocolBadge.textContent = String(inbound.protocol || '') + ' (' + String(inbound.transport || '') + ')';
      protocolCell.appendChild(protocolBadge);
      row.appendChild(protocolCell);

      const portCell = document.createElement('td');
      const port = document.createElement('code');
      port.style.fontSize = '0.95rem';
      port.style.color = 'var(--accent-warning)';
      port.textContent = String(inbound.port || '');
      portCell.appendChild(port);
      row.appendChild(portCell);

      const clientsCell = document.createElement('td');
      const clients = document.createElement('span');
      clients.className = 'badge badge-success';
      clients.style.cssText = 'background: rgba(16, 185, 129, 0.1); color: #34d399;';
      clients.textContent = String(Array.isArray(inbound.profiles) ? inbound.profiles.length : 0) + ' users';
      clientsCell.appendChild(clients);
      row.appendChild(clientsCell);

      const statusCell = document.createElement('td');
      const switchLabel = document.createElement('label');
      switchLabel.className = 'switch';
      const status = document.createElement('input');
      status.type = 'checkbox';
      status.dataset.adminOnly = 'true';
      status.checked = Boolean(inbound.enabled);
      status.addEventListener('change', () => toggleInboundActive(inbound.name, status.checked, status));
      const slider = document.createElement('span');
      slider.className = 'slider';
      switchLabel.appendChild(status);
      switchLabel.appendChild(slider);
      statusCell.appendChild(switchLabel);
      row.appendChild(statusCell);

      const actionsCell = document.createElement('td');
      actionsCell.style.textAlign = 'center';
      const dropdown = document.createElement('div');
      dropdown.className = 'dropdown';
      const dropdownButton = document.createElement('button');
      dropdownButton.type = 'button';
      dropdownButton.className = 'dropdown-btn';
      dropdownButton.style.padding = '6px 12px';
      dropdownButton.style.fontSize = '0.8rem';
      dropdownButton.textContent = 'Actions ▾';
      const content = document.createElement('div');
      content.className = 'dropdown-content';
      content.appendChild(createInboundActionButton('Edit Inbound', 'edit', inbound));
      content.appendChild(createInboundActionButton('Client Links', 'links', inbound));
      content.appendChild(createInboundActionButton('Delete', 'delete', inbound));
      dropdown.appendChild(dropdownButton);
      dropdown.appendChild(content);
      actionsCell.appendChild(dropdown);
      row.appendChild(actionsCell);

      return row;
    }

    async function loadInboundsIntoOutput() {
      const inbounds = await loadJSON('/api/inbounds', 'inbounds-output');
      if (!Array.isArray(inbounds)) return;
      window.cachedInbounds = inbounds;
      const tbody = document.getElementById('inbounds-tbody');
      if (!tbody) return;
      tbody.textContent = '';
      if (inbounds.length === 0) {
        appendInboundEmptyState(tbody);
        return;
      }
      inbounds.forEach((inbound) => tbody.appendChild(createInboundRow(inbound)));
      applyViewerRoleGuard();
    }

    function randomPassword() {
      const bytes = new Uint8Array(9);
      crypto.getRandomValues(bytes);
      return btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    }

    function genInboundPassword() {
      document.getElementById('inbound-password').value = randomPassword();
      scheduleInboundValidation();
    }

    window.genInboundNaivePassword = function() {
      const el = document.getElementById('inbound-naive-password');
      if (el) el.value = randomPassword();
      scheduleInboundValidation();
    };

    window.genInboundHysteria2Password = function() {
      const el = document.getElementById('inbound-hysteria2-password');
      if (el) el.value = randomPassword();
      scheduleInboundValidation();
    };

    window.renderDynamicProtocolFields = function(inbound) {
      const container = document.getElementById('inbound-protocol-fields');
      if (!container) return;
      const protocol = document.getElementById('inbound-protocol').value;
      const values = inbound && inbound.protocolFields ? inbound.protocolFields : {};
      ensureProtocolSchemas().then(() => {
        veilRenderDynamicProtocolFields(container, protocol, values);
        scheduleInboundValidation();
      }).catch((error) => {
        container.textContent = 'Could not load protocol fields: ' + String(error && error.message ? error.message : error);
        inboundValidationValid = false;
        const saveButton = document.getElementById('save-inbound');
        if (saveButton) saveButton.disabled = true;
      });
    };

    async function saveInbound(event) {
      event.preventDefault();
      if (!await validateInboundCandidate()) {
        return;
      }
      let payload;
      try {
        payload = buildInboundCandidate();
      } catch (err) {
        const summary = document.getElementById('inbound-validation-summary');
        if (summary) {
          summary.className = 'validation-summary validation-error';
          summary.textContent = 'Client profiles must be valid JSON: ' + String(err);
        }
        return;
      }
      const name = payload.name;
      if (!name) {
        document.getElementById('inbounds-output').textContent = veilT('inbounds.nameRequired');
        return;
      }
      const isEdit = window.inboundEditorMode === 'edit';
      const originalName = window.inboundEditorOriginalName || name;
      const saved = await loadJSON(isEdit ? '/api/inbounds/' + encodeURIComponent(originalName) : '/api/inbounds', 'inbounds-output', {
        method: isEdit ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!saved) {
        return;
      }
      closeInboundModal();
      await loadInboundsIntoOutput();
    }

    async function deleteInbound() {
      const name = document.getElementById('inbound-name').value.trim();
      if (!name) {
        document.getElementById('inbounds-output').textContent = veilT('inbounds.nameRequired');
        return;
      }
      if (confirm(veilT('confirm.deleteInbound', { name }))) {
        const deleted = await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
        if (!deleted) return;
        closeInboundModal();
        await loadInboundsIntoOutput();
      }
    }

    const inboundDropdownClones = new Map();

    function closeAllInboundDropdowns() {
      inboundDropdownClones.forEach((clone) => {
        if (clone.parentNode) {
          clone.parentNode.removeChild(clone);
        }
      });
      inboundDropdownClones.clear();
      document.querySelectorAll('#inbounds-table .dropdown.open').forEach((dropdown) => {
        dropdown.classList.remove('open');
      });
    }

    function openInboundDropdown(dropdown) {
      closeAllInboundDropdowns();
      const original = dropdown.querySelector('.dropdown-content');
      const btn = dropdown.querySelector('.dropdown-btn');
      if (!original || !btn) return;
      const clone = original.cloneNode(true);
      clone.style.display = 'block';
      clone.style.position = 'fixed';
      clone.style.zIndex = '1000';
      document.body.appendChild(clone);
      inboundDropdownClones.set(dropdown, clone);
      dropdown.classList.add('open');

      const rect = btn.getBoundingClientRect();
      const minWidth = Math.max(160, rect.width);
      let left = rect.right - minWidth;
      if (left + minWidth > window.innerWidth - 8) {
        left = window.innerWidth - minWidth - 8;
      }
      if (left < 8) {
        left = 8;
      }
      clone.style.top = rect.bottom + 'px';
      clone.style.left = left + 'px';
      clone.style.minWidth = minWidth + 'px';
    }

    document.addEventListener('click', (event) => {
      const actionButton = event.target.closest('[data-inbound-action]');
      if (actionButton) {
        const action = actionButton.dataset.inboundAction;
        const name = actionButton.dataset.inboundName || '';
        const protocol = actionButton.dataset.inboundProtocol || '';
        closeAllInboundDropdowns();
        if (action === 'edit') {
          openEditInboundModal(name);
        } else if (action === 'links') {
          openClientLinksModalFor(name, protocol);
        } else if (action === 'delete') {
          directDeleteInbound(name);
        }
        event.stopPropagation();
        return;
      }

      const btn = event.target.closest('#inbounds-table .dropdown-btn');
      if (btn) {
        const dropdown = btn.closest('.dropdown');
        const wasOpen = dropdown && dropdown.classList.contains('open');
        if (dropdown && !wasOpen) {
          openInboundDropdown(dropdown);
        } else {
          closeAllInboundDropdowns();
        }
        event.stopPropagation();
        return;
      }
      closeAllInboundDropdowns();
    });

    window.addEventListener('resize', closeAllInboundDropdowns);
    window.addEventListener('scroll', closeAllInboundDropdowns, true);

    window.addEventListener('DOMContentLoaded', () => {
      const form = document.getElementById('inbound-form');
      if (form) {
        form.addEventListener('input', scheduleInboundValidation);
        form.addEventListener('change', scheduleInboundValidation);
      }
      setTimeout(loadInboundsIntoOutput, 500);
    });`
}
