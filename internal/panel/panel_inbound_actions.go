package panel

const panelInboundActionsPlaceholder = "__VEIL_PANEL_INBOUND_ACTIONS__"

func panelInboundActionsJS() string {
	return panelInboundProtocolTransportRulesJS() + `    // Global cash and modals handlers
    window.cachedInbounds = [];
    window.cachedSettings = window.cachedSettings || null;
    window.cachedWarp = window.cachedWarp || null;

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
        const existingIndex = inbounds.findIndex((item) => item.name === candidate.name);
        if (existingIndex >= 0) {
          inbounds[existingIndex] = candidate;
        } else {
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
      
      // Clear client profile helpers
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

      // Clear client profile helpers
      document.getElementById('client-profile-name').value = '';
      document.getElementById('client-profile-username').value = '';
      document.getElementById('client-profile-password').value = '';

      document.getElementById('delete-inbound').style.display = 'block';
      renderDynamicProtocolFields(inbound);
      
      openVeilDialog(document.getElementById('inbound-modal-overlay'));
      clearInboundValidation();
      scheduleInboundValidation();
    };

    window.toggleInboundActive = async function(name, checked) {
      const inbound = window.cachedInbounds.find(i => i.name === name);
      if (!inbound) return;
      const payload = Object.assign({}, inbound, { enabled: checked });
      await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      loadInboundsIntoOutput();
    };

    window.directDeleteInbound = async function(name) {
      if (confirm(veilT('confirm.deleteInbound', { name }))) {
        await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
        loadInboundsIntoOutput();
      }
    };

    async function loadInboundsIntoOutput() {
      const inbounds = await loadJSON('/api/inbounds', 'inbounds-output');
      if (Array.isArray(inbounds)) {
        window.cachedInbounds = inbounds;
        const tbody = document.getElementById('inbounds-tbody');
        tbody.innerHTML = '';
        if (inbounds.length === 0) {
          tbody.innerHTML = '<tr><td colspan="6" style="text-align: center; color: var(--text-muted); padding: 24px;">No inbounds configured. Click Add Inbound to start.</td></tr>';
          return;
        }
        inbounds.forEach(inbound => {
          const clientCount = inbound.profiles ? inbound.profiles.length : 0;
          const statusChecked = inbound.enabled ? 'checked' : '';
          
          let badgeStyle = 'background: rgba(79, 70, 229, 0.15); color: #818cf8; border: 1px solid rgba(79, 70, 229, 0.3);';
          if (inbound.protocol === 'hysteria2') {
            badgeStyle = 'background: rgba(139, 92, 246, 0.15); color: #a78bfa; border: 1px solid rgba(139, 92, 246, 0.3);';
          } else if (inbound.protocol === 'mieru') {
            badgeStyle = 'background: rgba(59, 130, 246, 0.15); color: #60a5fa; border: 1px solid rgba(59, 130, 246, 0.3);';
          } else if (inbound.protocol === 'olcrtc') {
            badgeStyle = 'background: rgba(16, 185, 129, 0.15); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.3);';
          }
          
          const row = document.createElement('tr');
          row.innerHTML = '<td><strong style="color: #fff; font-size: 0.95rem;">' + inbound.name + '</strong></td>' +
            '<td><span class="badge" style="' + badgeStyle + '">' + inbound.protocol + ' (' + inbound.transport + ')</span></td>' +
            '<td><code style="font-size: 0.95rem; color: var(--accent-warning);">' + inbound.port + '</code></td>' +
            '<td><span class="badge badge-success" style="background: rgba(16, 185, 129, 0.1); color: #34d399;">' + clientCount + ' users</span></td>' +
            '<td>' +
              '<label class="switch">' +
                '<input type="checkbox" data-admin-only="true" ' + statusChecked + ' onchange="toggleInboundActive(\'' + inbound.name + '\', this.checked)">' +
                '<span class="slider"></span>' +
              '</label>' +
            '</td>' +
            '<td style="text-align: center;">' +
              '<div class="dropdown">' +
                '<button type="button" class="dropdown-btn" style="padding: 6px 12px; font-size: 0.8rem;">Actions ▾</button>' +
                '<div class="dropdown-content">' +
                  '<button type="button" data-admin-only="true" onclick="openEditInboundModal(\'' + inbound.name + '\')">Edit Inbound</button>' +
                  '<button type="button" onclick="openClientLinksModalFor(\'' + inbound.name + '\', \'' + inbound.protocol + '\')">Client Links</button>' +
                  '<button type="button" data-admin-only="true" onclick="directDeleteInbound(\'' + inbound.name + '\')" style="color: var(--accent-danger);">Delete</button>' +
                '</div>' +
              '</div>' +
            '</td>';
          tbody.appendChild(row);
        });
        applyViewerRoleGuard();
      }
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
      const exists = Array.isArray(window.cachedInbounds) && window.cachedInbounds.some((inbound) => inbound.name === name);

      const saved = await loadJSON(exists ? '/api/inbounds/' + encodeURIComponent(name) : '/api/inbounds', 'inbounds-output', {
        method: exists ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!saved) {
        return;
      }
      closeInboundModal();
      loadInboundsIntoOutput();
    }

    async function deleteInbound() {
      const name = document.getElementById('inbound-name').value.trim();
      if (!name) {
        document.getElementById('inbounds-output').textContent = veilT('inbounds.nameRequired');
        return;
      }
      if (confirm(veilT('confirm.deleteInbound', { name }))) {
        await loadJSON('/api/inbounds/' + encodeURIComponent(name), 'inbounds-output', { method: 'DELETE' });
        closeInboundModal();
        loadInboundsIntoOutput();
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
      if (event.target.closest('.dropdown-content') && event.target.closest('button')) {
        closeAllInboundDropdowns();
        return;
      }
      closeAllInboundDropdowns();
    });

    window.addEventListener('resize', closeAllInboundDropdowns);
    window.addEventListener('scroll', closeAllInboundDropdowns, true);

    // Auto load inbounds list on startup
    window.addEventListener('DOMContentLoaded', () => {
      const form = document.getElementById('inbound-form');
      if (form) {
        form.addEventListener('input', scheduleInboundValidation);
        form.addEventListener('change', scheduleInboundValidation);
      }
      setTimeout(loadInboundsIntoOutput, 500);
    });`
}
