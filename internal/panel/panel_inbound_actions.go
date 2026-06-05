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
        summary.textContent = 'Validation runs as fields change.';
      }
    }

    function inboundFieldControl(field) {
      const normalized = String(field || '').replace(/^inbounds\[[0-9]+\]\./, '');
      const ids = {
        name: 'inbound-name',
        protocol: 'inbound-protocol',
        transport: 'inbound-transport',
        port: 'inbound-port',
        password: 'inbound-password',
        naivePassword: 'inbound-naive-password',
        hysteria2Password: 'inbound-hysteria2-password',
        olcrtcRoomID: 'inbound-olcrtc-room-id'
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
          message.textContent = issue.message + (issue.remediation ? ' ' + issue.remediation : '');
        }
      });
      if (summary) {
        summary.className = errors.length > 0 ? 'validation-summary validation-error' : 'validation-summary validation-ok';
        if (issues.length === 0) {
          summary.textContent = 'Configuration is ready to save.';
        } else {
          summary.textContent = issues.map((issue) => issue.message + (issue.remediation ? ' ' + issue.remediation : '')).join(' ');
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
      if (protocol === 'naiveproxy') {
        const usernameEl = document.getElementById('inbound-naive-username');
        const passwordEl = document.getElementById('inbound-naive-password');
        const fallbackEl = document.getElementById('inbound-fallback-root');
        if (usernameEl) payload.naiveUsername = usernameEl.value.trim();
        if (passwordEl) payload.naivePassword = passwordEl.value.trim();
        if (fallbackEl) payload.fallbackRoot = fallbackEl.value.trim();
      } else if (protocol === 'hysteria2') {
        const passwordEl = document.getElementById('inbound-hysteria2-password');
        const masqueradeEl = document.getElementById('inbound-masquerade-url');
        if (passwordEl) payload.hysteria2Password = passwordEl.value.trim();
        if (masqueradeEl) payload.masqueradeURL = masqueradeEl.value.trim();
      } else if (protocol === 'olcrtc') {
        const authEl = document.getElementById('inbound-olcrtc-auth');
        const transportEl = document.getElementById('inbound-olcrtc-transport');
        const roomEl = document.getElementById('inbound-olcrtc-room-id');
        if (authEl) payload.olcrtcAuth = authEl.value.trim();
        if (transportEl) payload.olcrtcTransport = transportEl.value.trim();
        if (roomEl) payload.olcrtcRoomID = roomEl.value.trim();
      }
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
      document.getElementById('inbound-protocol').value = 'naiveproxy';
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
                  '<button type="button" onclick="openClientLinksModalFor(\'' + inbound.name + '\')">Client Links</button>' +
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

    window.genInboundOlcrtcRoomID = function() {
      const authSelect = document.getElementById('inbound-olcrtc-auth');
      const auth = authSelect ? authSelect.value : 'jitsi';
      const el = document.getElementById('inbound-olcrtc-room-id');
      if (!el) return;
      if (auth === 'jitsi') {
        el.value = 'jitsi-room-' + Math.floor(100000 + Math.random() * 900000);
      } else if (auth === 'livekit') {
        el.value = 'livekit-room-' + Math.floor(100000 + Math.random() * 900000);
      } else {
        el.value = 'room-' + Math.floor(100000 + Math.random() * 900000);
      }
      scheduleInboundValidation();
    };

    window.renderDynamicProtocolFields = function(inbound) {
      const container = document.getElementById('inbound-protocol-fields');
      if (!container) return;
      
      const protocol = document.getElementById('inbound-protocol').value;
      
      let html = '';
      if (protocol === 'naiveproxy') {
        html = '<div class="form-grid" style="margin: 0; padding: 0;">' +
          '<div>' +
            '<label for="inbound-naive-username">Naive Username</label>' +
            '<input id="inbound-naive-username" autocomplete="off" placeholder="veil">' +
          '</div>' +
          '<div>' +
            '<label for="inbound-naive-password">Naive Password</label>' +
            '<div style="display:flex;gap:8px">' +
              '<input id="inbound-naive-password" type="text" autocomplete="off" placeholder="password" style="flex:1">' +
              '<button type="button" class="secondary" onclick="genInboundNaivePassword()" style="white-space:nowrap; padding: 12px 14px;">Generate</button>' +
            '</div>' +
          '</div>' +
          '<div style="grid-column: 1 / -1">' +
            '<label for="inbound-fallback-root">Fallback Root</label>' +
            '<input id="inbound-fallback-root" autocomplete="off" placeholder="/var/lib/veil/www">' +
          '</div>' +
        '</div>';
      } else if (protocol === 'hysteria2') {
        html = '<div class="form-grid" style="margin: 0; padding: 0;">' +
          '<div>' +
            '<label for="inbound-hysteria2-password">Hysteria2 Password</label>' +
            '<div style="display:flex;gap:8px">' +
              '<input id="inbound-hysteria2-password" type="text" autocomplete="off" placeholder="password" style="flex:1">' +
              '<button type="button" class="secondary" onclick="genInboundHysteria2Password()" style="white-space:nowrap; padding: 12px 14px;">Generate</button>' +
            '</div>' +
          '</div>' +
          '<div>' +
            '<label for="inbound-masquerade-url">Masquerade URL</label>' +
            '<input id="inbound-masquerade-url" autocomplete="off" placeholder="https://example.com">' +
          '</div>' +
        '</div>';
      } else if (protocol === 'olcrtc') {
        html = '<div class="form-grid" style="margin: 0; padding: 0;">' +
          '<div>' +
            '<label for="inbound-olcrtc-auth">olcRTC Auth Provider</label>' +
            '<select id="inbound-olcrtc-auth" onchange="genInboundOlcrtcRoomID()">' +
              '<option value="jitsi">jitsi</option>' +
              '<option value="livekit">livekit</option>' +
              '<option value="external">external</option>' +
            '</select>' +
          '</div>' +
          '<div>' +
            '<label for="inbound-olcrtc-transport">olcRTC Transport</label>' +
            '<select id="inbound-olcrtc-transport">' +
              '<option value="datachannel">datachannel</option>' +
              '<option value="websocket">websocket</option>' +
              '<option value="quic">quic</option>' +
            '</select>' +
          '</div>' +
          '<div style="grid-column: 1 / -1">' +
            '<label for="inbound-olcrtc-room-id">olcRTC Room ID</label>' +
            '<div style="display:flex;gap:8px">' +
              '<input id="inbound-olcrtc-room-id" autocomplete="off" placeholder="Room ID" style="flex:1">' +
              '<button type="button" class="secondary" onclick="genInboundOlcrtcRoomID()" style="white-space:nowrap; padding: 12px 14px;">Generate Room ID</button>' +
            '</div>' +
          '</div>' +
        '</div>';
      }
      
      container.innerHTML = html;
      
      // Populate fields
      if (inbound) {
        if (protocol === 'naiveproxy') {
          document.getElementById('inbound-naive-username').value = inbound.naiveUsername || '';
          document.getElementById('inbound-naive-password').value = inbound.naivePassword || '';
          document.getElementById('inbound-fallback-root').value = inbound.fallbackRoot || '';
        } else if (protocol === 'hysteria2') {
          document.getElementById('inbound-hysteria2-password').value = inbound.hysteria2Password || '';
          document.getElementById('inbound-masquerade-url').value = inbound.masqueradeURL || '';
        } else if (protocol === 'olcrtc') {
          document.getElementById('inbound-olcrtc-auth').value = inbound.olcrtcAuth || 'jitsi';
          document.getElementById('inbound-olcrtc-transport').value = inbound.olcrtcTransport || 'datachannel';
          document.getElementById('inbound-olcrtc-room-id').value = inbound.olcrtcRoomID || '';
        }
      } else {
        // Defaults for Add
        if (protocol === 'naiveproxy') {
          document.getElementById('inbound-naive-username').value = 'veil';
          document.getElementById('inbound-naive-password').value = randomPassword();
          document.getElementById('inbound-fallback-root').value = '/var/lib/veil/www';
        } else if (protocol === 'hysteria2') {
          document.getElementById('inbound-hysteria2-password').value = randomPassword();
          document.getElementById('inbound-masquerade-url').value = 'https://example.com';
        } else if (protocol === 'olcrtc') {
          document.getElementById('inbound-olcrtc-auth').value = 'jitsi';
          document.getElementById('inbound-olcrtc-transport').value = 'datachannel';
          genInboundOlcrtcRoomID();
        }
      }
      scheduleInboundValidation();
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
