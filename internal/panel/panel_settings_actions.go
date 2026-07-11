package panel

const panelSettingsActionsPlaceholder = "__VEIL_PANEL_SETTINGS_ACTIONS__"

func panelSettingsActionsJS() string {
	return `    function settingsProtocolFieldElementId(key) {
      return 'settings-protocol-field-' + key;
    }

    function allSettingsFieldSchemas() {
      const fields = [];
      const seen = new Set();
      const schemas = window.protocolSchemas || {};
      Object.keys(schemas).forEach((protocol) => {
        const info = schemas[protocol];
        (info && Array.isArray(info.settingsFieldSchema) ? info.settingsFieldSchema : []).forEach((field) => {
          if (!field || !field.key || seen.has(field.key)) return;
          seen.add(field.key);
          fields.push({ protocol, field });
        });
      });
      return fields;
    }

    function createSettingsProtocolInput(field) {
      const id = settingsProtocolFieldElementId(field.key);
      let input;
      if (field.type === 'select') {
        input = document.createElement('select');
        (field.options || []).forEach((optionInfo) => {
          const option = document.createElement('option');
          option.value = optionInfo.value;
          option.textContent = optionInfo.label;
          input.appendChild(option);
        });
      } else {
        input = document.createElement('input');
        if (field.type === 'checkbox') {
          input.type = 'checkbox';
          input.style.width = 'auto';
        } else if (field.type === 'number') {
          input.type = 'number';
        } else if (field.type === 'password') {
          input.type = 'password';
          input.autocomplete = 'new-password';
        } else {
          input.type = 'text';
          input.autocomplete = 'off';
        }
      }
      input.id = id;
      input.dataset.settingsProtocolKey = field.key;
      input.dataset.settingsProtocolType = field.type || 'text';
      if (field.required) input.required = true;
      if (field.placeholder) input.placeholder = field.placeholder;
      return input;
    }

    function renderSettingsProtocolFields(values) {
      const container = document.getElementById('settings-protocol-fields');
      if (!container) return;
      container.textContent = '';
      const entries = allSettingsFieldSchemas();
      if (entries.length === 0) {
        const message = document.createElement('p');
        message.className = 'hint';
        message.textContent = 'No protocol-specific global settings are registered.';
        container.appendChild(message);
        return;
      }
      const source = values && typeof values === 'object' ? values : {};
      let currentProtocol = '';
      let grid = null;
      entries.forEach(({ protocol, field }) => {
        if (protocol !== currentProtocol) {
          currentProtocol = protocol;
          const heading = document.createElement('h3');
          heading.textContent = (window.protocolSchemas[protocol].displayName || protocol) + ' settings';
          container.appendChild(heading);
          grid = document.createElement('div');
          grid.className = 'form-grid';
          container.appendChild(grid);
        }
        const wrapper = document.createElement('div');
        const input = createSettingsProtocolInput(field);
        if (field.type === 'checkbox') {
          const label = document.createElement('label');
          label.htmlFor = input.id;
          label.style.display = 'flex';
          label.style.alignItems = 'center';
          label.style.gap = '8px';
          label.appendChild(input);
          label.appendChild(document.createTextNode(field.label));
          wrapper.appendChild(label);
        } else {
          const label = document.createElement('label');
          label.htmlFor = input.id;
          label.textContent = field.label;
          wrapper.appendChild(label);
          wrapper.appendChild(input);
        }
        if (field.generateAction === 'password') {
          const row = document.createElement('div');
          row.style.display = 'flex';
          row.style.gap = '8px';
          input.style.flex = '1';
          wrapper.removeChild(input);
          row.appendChild(input);
          const button = document.createElement('button');
          button.type = 'button';
          button.className = 'secondary';
          button.textContent = 'Generate';
          button.addEventListener('click', () => {
            input.value = randomPassword();
            input.dispatchEvent(new Event('input', { bubbles: true }));
          });
          row.appendChild(button);
          wrapper.appendChild(row);
        }
        const value = source[field.key] !== undefined ? source[field.key] : field.default;
        if (field.type === 'checkbox') {
          input.checked = Boolean(value);
        } else if (value !== undefined && value !== null) {
          input.value = String(value);
        }
        grid.appendChild(wrapper);
      });
      applyViewerRoleGuard();
    }

    function collectSettingsProtocolFields() {
      const protocolFields = {};
      document.querySelectorAll('[data-settings-protocol-key]').forEach((input) => {
        const key = input.dataset.settingsProtocolKey;
        const type = input.dataset.settingsProtocolType;
        if (type === 'checkbox') {
          protocolFields[key] = input.checked;
        } else if (type === 'number') {
          const raw = input.value.trim();
          if (raw === '') {
            protocolFields[key] = '';
          } else {
            const value = Number(raw);
            protocolFields[key] = Number.isFinite(value) ? value : '';
          }
        } else {
          protocolFields[key] = input.value.trim();
        }
      });
      return protocolFields;
    }

    async function loadSettingsIntoForm() {
      if (!document.getElementById('settings-form')) return;
      const data = await loadJSON('/api/settings', 'settings-output');
      if (!data) return;
      window.cachedSettings = data;
      document.getElementById('settings-panel-listen').value = data.panelListen || '';
      document.getElementById('settings-mode').value = data.mode || '';
      document.getElementById('settings-panel-access').value = data.panelAccess || 'local';
      document.getElementById('settings-web-base-path').value = data.webBasePath || '';
      document.getElementById('settings-domain').value = data.domain || '';
      document.getElementById('settings-email').value = data.email || '';
      try {
        await ensureProtocolSchemas();
        const values = Object.assign({}, data, data.protocolFields || {});
        renderSettingsProtocolFields(values);
      } catch (error) {
        const container = document.getElementById('settings-protocol-fields');
        if (container) container.textContent = 'Could not load protocol settings: ' + String(error && error.message ? error.message : error);
      }
    }

    async function saveSettings(event) {
      if (!document.getElementById('settings-form')) return;
      event.preventDefault();
      const form = document.getElementById('settings-form');
      if (!form.checkValidity()) {
        form.reportValidity();
        return;
      }
      const payload = {
        panelListen: document.getElementById('settings-panel-listen').value.trim(),
        panelAccess: document.getElementById('settings-panel-access').value,
        webBasePath: document.getElementById('settings-web-base-path').value.trim(),
        mode: document.getElementById('settings-mode').value.trim(),
        domain: document.getElementById('settings-domain').value.trim(),
        email: document.getElementById('settings-email').value.trim(),
        protocolFields: collectSettingsProtocolFields()
      };
      const saved = await loadJSON('/api/settings', 'settings-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (saved) {
        window.cachedSettings = saved;
        const values = Object.assign({}, saved, saved.protocolFields || {});
        renderSettingsProtocolFields(values);
      }
    }`
}
