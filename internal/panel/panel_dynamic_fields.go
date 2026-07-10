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
          return map;
        });
      return window.protocolSchemaPromise;
    }

    function schemaForProtocol(protocol) {
      return window.protocolSchemas ? window.protocolSchemas[protocol] : null;
    }

    function protocolFieldElementId(key) {
      return 'protocol-field-' + key;
    }

    function renderProtocolFieldInput(field) {
      const id = protocolFieldElementId(field.key);
      if (field.type === 'select') {
        const options = (field.options || []).map((opt) => {
          const attrs = [];
          if (opt.attributes) {
            Object.entries(opt.attributes).forEach(([k, v]) => attrs.push(' data-' + k + '="' + String(v).replace(/"/g, '&quot;') + '"'));
          }
          return '<option value="' + String(opt.value).replace(/"/g, '&quot;') + '"' + attrs.join('') + '>' + String(opt.label).replace(/</g, '&lt;') + '</option>';
        }).join('');
        return '<select id="' + id + '" autocomplete="off">' + options + '</select>';
      }
      if (field.type === 'checkbox') {
        return '<label for="' + id + '" style="display:flex;align-items:center;gap:8px;cursor:pointer;">' +
          '<input id="' + id + '" type="checkbox" style="width:auto">' +
          String(field.label).replace(/</g, '&lt;') + '</label>';
      }
      if (field.type === 'number') {
        return '<input id="' + id + '" type="number" autocomplete="off">';
      }
      const inputType = field.type === 'password' ? 'password' : 'text';
      const placeholder = field.placeholder ? ' placeholder="' + String(field.placeholder).replace(/"/g, '&quot;') + '"' : '';
      return '<input id="' + id + '" type="' + inputType + '" autocomplete="off"' + placeholder + '>';
    }

    window.veilRenderDynamicProtocolFields = function(container, protocol, values) {
      const info = schemaForProtocol(protocol);
      container.innerHTML = '';
      if (!info || !Array.isArray(info.inboundFieldSchema) || info.inboundFieldSchema.length === 0) {
        return;
      }
      const grid = document.createElement('div');
      grid.className = 'form-grid';
      grid.style = 'margin:0;padding:0';

      info.inboundFieldSchema.forEach((field) => {
        const wrapper = document.createElement('div');
        const id = protocolFieldElementId(field.key);
        if (field.type !== 'checkbox') {
          const label = document.createElement('label');
          label.htmlFor = id;
          label.textContent = field.label;
          wrapper.appendChild(label);
        }

        let inputHtml = renderProtocolFieldInput(field);
        if (field.generateAction) {
          const row = document.createElement('div');
          row.style = 'display:flex;gap:8px';
          const tmp = document.createElement('div'); tmp.innerHTML = inputHtml;
          const inputEl = tmp.firstElementChild;
          inputEl.style.flex = '1';
          row.appendChild(inputEl);
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'secondary';
          btn.id = protocolFieldElementId(field.key) + '-generate';
          btn.style = 'white-space:nowrap;padding:12px 14px';
          btn.textContent = 'Generate';
          btn.onclick = function() { window.veilGenerateProtocolField(protocol, field.key, field.generateAction, field.generateActionField); };
          row.appendChild(btn);
          wrapper.appendChild(row);
        } else {
          const tmp = document.createElement('div'); tmp.innerHTML = inputHtml;
          wrapper.appendChild(tmp.firstElementChild);
        }
        grid.appendChild(wrapper);
      });

      container.appendChild(grid);

      const src = values && typeof values === 'object' ? values : {};
      info.inboundFieldSchema.forEach((field) => {
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
        if (field.generateAction !== 'room' || !field.generateActionField) return;
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
      } else if (action === 'room') {
        const authEl = actionField ? document.getElementById(protocolFieldElementId(actionField)) : null;
        const provider = authEl ? authEl.value : '';
        try {
          const resp = await fetch('/api/' + encodeURIComponent(protocol) + '/room', {
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
