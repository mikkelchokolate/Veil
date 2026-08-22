package panel

const panelDynamicFieldsPlaceholder = "__VEIL_PANEL_DYNAMIC_FIELDS__"

func panelDynamicFieldsJS() string {
	return `    // Protocol field schemas, loaded once from /api/protocols.
    window.protocolSchemas = window.protocolSchemas || null;
    window.protocolSchemaPromise = window.protocolSchemaPromise || null;

    function ensureProtocolSchemas() {
      if (window.protocolSchemas) return Promise.resolve(window.protocolSchemas);
      if (window.protocolSchemaPromise) return window.protocolSchemaPromise;
      window.protocolSchemaPromise = fetch('/api/protocols', { headers: authHeaders() })
        .then((response) => {
          if (!response.ok) throw new Error('Failed to load protocol schemas');
          return response.json();
        })
        .then((data) => {
          const map = {};
          (Array.isArray(data) ? data : []).forEach((item) => {
            if (item && item.protocol) map[item.protocol] = item;
          });
          window.protocolSchemas = map;
          window.protocolSchemaPromise = null;
          return map;
        })
        .catch((err) => {
          // A rejected cached promise would permanently poison dynamic forms.
          // Clear it so the next modal open can retry after a transient outage.
          window.protocolSchemaPromise = null;
          throw err;
        });
      return window.protocolSchemaPromise;
    }

    function schemaForProtocol(protocol) {
      return window.protocolSchemas ? window.protocolSchemas[protocol] : null;
    }

    function protocolFieldElementId(key) {
      return 'protocol-field-' + String(key || '');
    }

    function protocolOptionAttributeName(name) {
      const normalized = String(name || '').trim().toLowerCase();
      if (!normalized) return '';
      const candidate = normalized.indexOf('data-') === 0 ? normalized : 'data-' + normalized;
      return /^data-[a-z0-9_.:-]+$/.test(candidate) ? candidate : '';
    }

    function createProtocolFieldInput(field) {
      const id = protocolFieldElementId(field.key);
      let input;
      if (field.type === 'select') {
        input = document.createElement('select');
        (Array.isArray(field.options) ? field.options : []).forEach((optionInfo) => {
          const option = document.createElement('option');
          option.value = String(optionInfo && optionInfo.value !== undefined ? optionInfo.value : '');
          option.textContent = String(optionInfo && optionInfo.label !== undefined ? optionInfo.label : option.value);
          if (optionInfo && optionInfo.attributes && typeof optionInfo.attributes === 'object') {
            Object.entries(optionInfo.attributes).forEach(([name, value]) => {
              const attributeName = protocolOptionAttributeName(name);
              if (attributeName) option.setAttribute(attributeName, String(value));
            });
          }
          input.appendChild(option);
        });
      } else {
        input = document.createElement('input');
        if (field.type === 'checkbox') {
          input.type = 'checkbox';
          input.style.width = 'auto';
        } else if (field.type === 'number') {
          input.type = 'number';
          input.autocomplete = 'off';
        } else if (field.type === 'password') {
          input.type = 'password';
          input.autocomplete = 'new-password';
        } else {
          input.type = 'text';
          input.autocomplete = 'off';
        }
      }
      input.id = id;
      input.required = Boolean(field.required);
      if (field.placeholder) input.placeholder = String(field.placeholder);
      return input;
    }

    window.veilRenderDynamicProtocolFields = function(container, protocol, values) {
      const info = schemaForProtocol(protocol);
      container.textContent = '';
      if (!info || !Array.isArray(info.inboundFieldSchema) || info.inboundFieldSchema.length === 0) {
        return;
      }
      const grid = document.createElement('div');
      grid.className = 'form-grid';
      grid.style.margin = '0';
      grid.style.padding = '0';

      info.inboundFieldSchema.forEach((field) => {
        if (!field || !field.key) return;
        const wrapper = document.createElement('div');
        const inputEl = createProtocolFieldInput(field);
        if (field.type === 'checkbox') {
          const label = document.createElement('label');
          label.htmlFor = inputEl.id;
          label.style.display = 'flex';
          label.style.alignItems = 'center';
          label.style.gap = '8px';
          label.style.cursor = 'pointer';
          label.appendChild(inputEl);
          label.appendChild(document.createTextNode(String(field.label || field.key)));
          wrapper.appendChild(label);
        } else {
          const label = document.createElement('label');
          label.htmlFor = inputEl.id;
          label.textContent = String(field.label || field.key);
          wrapper.appendChild(label);
          if (field.generateAction) {
            const row = document.createElement('div');
            row.style.display = 'flex';
            row.style.gap = '8px';
            inputEl.style.flex = '1';
            row.appendChild(inputEl);
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'secondary';
            btn.id = protocolFieldElementId(field.key) + '-generate';
            btn.style.whiteSpace = 'nowrap';
            btn.style.padding = '12px 14px';
            btn.textContent = 'Generate';
            btn.addEventListener('click', () => {
              window.veilGenerateProtocolField(protocol, field.key, field.generateAction, field.generateActionField);
            });
            row.appendChild(btn);
            wrapper.appendChild(row);
          } else {
            wrapper.appendChild(inputEl);
          }
        }
        grid.appendChild(wrapper);
      });

      container.appendChild(grid);

      const src = values && typeof values === 'object' ? values : {};
      info.inboundFieldSchema.forEach((field) => {
        if (!field || !field.key) return;
        const el = document.getElementById(protocolFieldElementId(field.key));
        if (!el) return;
        const val = src[field.key] !== undefined ? src[field.key] : (field.default !== undefined ? field.default : '');
        if (field.type === 'checkbox') {
          el.checked = !!val;
        } else {
          el.value = val;
        }
      });

      // Wire provider-aware generate buttons (e.g. olcRTC room generation).
      info.inboundFieldSchema.forEach((field) => {
        if (!field || field.generateAction !== 'room' || !field.generateActionField) return;
        const authEl = document.getElementById(protocolFieldElementId(field.generateActionField));
        const btn = document.getElementById(protocolFieldElementId(field.key) + '-generate');
        if (!authEl || !btn) return;
        function updateRoomGenerateButton() {
          const opt = authEl.selectedOptions[0];
          const auto = !!(opt && opt.dataset.autoroom === 'true');
          btn.disabled = !auto;
          btn.title = auto ? '' : 'This provider needs a room created on its website first; auto-generate is unavailable.';
        }
        authEl.addEventListener('change', updateRoomGenerateButton);
        updateRoomGenerateButton();
      });
    };

    window.veilCollectProtocolFields = function(protocol) {
      const info = schemaForProtocol(protocol);
      const out = {};
      if (!info || !Array.isArray(info.inboundFieldSchema)) return out;
      info.inboundFieldSchema.forEach((field) => {
        const el = document.getElementById(protocolFieldElementId(field.key));
        if (!el) return;
        if (field.type === 'checkbox') {
          out[field.key] = el.checked;
        } else if (field.type === 'number') {
          const num = parseFloat(el.value);
          out[field.key] = isNaN(num) ? '' : num;
        } else {
          out[field.key] = el.value.trim();
        }
      });
      return out;
    };

    window.veilGenerateProtocolField = async function(protocol, key, action, actionField) {
      const el = document.getElementById(protocolFieldElementId(key));
      if (!el) return;
      if (action === 'password') {
        el.value = randomPassword();
      } else if (action === 'hex64') {
        // 32 random bytes -> 64 lowercase hex chars (mirrors the React SPA
        // generateFieldValue). olcRTC encryption keys use this shape; the
        // legacy panel previously had no handler for it (audit #38/#48).
        const bytes = new Uint8Array(32);
        crypto.getRandomValues(bytes);
        el.value = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
      } else if (action === 'room') {
        const authEl = actionField ? document.getElementById(protocolFieldElementId(actionField)) : null;
        const provider = authEl ? authEl.value : '';
        try {
          const resp = await fetch('/api/protocols/' + encodeURIComponent(protocol) + '/room', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ provider })
          });
          if (!resp.ok) return;
          const data = await resp.json();
          el.value = data.roomID || '';
        } catch (_) {}
      }
      scheduleInboundValidation();
    };

    window.veilProtocolFieldControl = function(fieldPath) {
      const prefix = 'protocolFields.';
      if (fieldPath && fieldPath.indexOf(prefix) === 0) {
        return document.getElementById(protocolFieldElementId(fieldPath.substring(prefix.length)));
      }
      return null;
    };

    // Patch the static inboundFieldControl so validation issues pointing at
    // protocolFields.* highlight the rendered dynamic inputs.
    const originalInboundFieldControl = inboundFieldControl;
    inboundFieldControl = function(field) {
      const normalized = String(field || '').replace(/^inbounds\[[0-9]+\]\./, '');
      const dynamic = veilProtocolFieldControl(normalized);
      if (dynamic) return dynamic;
      return originalInboundFieldControl(field);
    };

    // Start loading schemas early so modal opens are snappy.
    window.addEventListener('DOMContentLoaded', ensureProtocolSchemas);
`
}
