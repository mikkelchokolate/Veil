package panel

const panelRoutingActionsPlaceholder = "__VEIL_PANEL_ROUTING_ACTIONS__"

func panelRoutingActionsJS() string {
	return `    let routingModalGeneration = 0;
    let routingRulesLoadGeneration = 0;
    let routingRulesLoadController = null;
    let routingMutationInFlight = false;

    function setRoutingMutationControlsDisabled(disabled) {
      const busy = Boolean(disabled);
      const viewer = isViewerRole();
      ['save-routing-rule', 'delete-routing-rule', 'apply-routing-preset', 'add-routing-rule-btn'].forEach((id) => {
        const button = document.getElementById(id);
        if (button) button.disabled = busy || viewer;
      });
      const loadButton = document.getElementById('load-routing-rules');
      if (loadButton) loadButton.disabled = busy;
      document.querySelectorAll('[data-routing-mutation="true"]').forEach((control) => {
        control.disabled = busy || viewer;
      });
    }

    function cancelRoutingRulesLoad() {
      routingRulesLoadGeneration += 1;
      if (routingRulesLoadController) {
        routingRulesLoadController.abort();
        routingRulesLoadController = null;
      }
    }

    async function withRoutingMutation(action) {
      if (routingMutationInFlight) return null;
      routingMutationInFlight = true;
      cancelRoutingRulesLoad();
      setRoutingMutationControlsDisabled(true);
      try {
        return await action();
      } finally {
        routingMutationInFlight = false;
        setRoutingMutationControlsDisabled(false);
        applyViewerRoleGuard();
      }
    }

    async function openRoutingModal(rule) {
      const generation = ++routingModalGeneration;
      const modal = document.getElementById('routing-modal');
      const title = document.getElementById('modal-title');
      const nameInput = document.getElementById('routing-rule-name');
      const matchInput = document.getElementById('routing-rule-match');
      const outboundSelect = document.getElementById('routing-rule-outbound');
      const enabledCheckbox = document.getElementById('routing-rule-enabled');
      const deleteBtn = document.getElementById('delete-routing-rule');

      if (!modal) return;

      // Query WARP status dynamically to decide if we add "warp". A generation
      // guard prevents a late response from an older modal open overwriting the
      // rule the operator selected most recently.
      let warpEnabled = false;
      try {
        const response = await fetch('/api/warp', { headers: authHeaders() });
        if (response.ok) {
          const warpData = await response.json();
          warpEnabled = Boolean(warpData.enabled);
        }
      } catch (err) {
        console.error('Failed to query WARP status:', err);
      }
      if (generation !== routingModalGeneration) return;

      outboundSelect.textContent = '';
      const optionDirect = document.createElement('option');
      optionDirect.value = 'direct';
      optionDirect.textContent = 'direct';
      outboundSelect.appendChild(optionDirect);

      const optionProxy = document.createElement('option');
      optionProxy.value = 'proxy';
      optionProxy.textContent = 'proxy';
      outboundSelect.appendChild(optionProxy);

      if (warpEnabled) {
        const optionWarp = document.createElement('option');
        optionWarp.value = 'warp';
        optionWarp.textContent = 'warp';
        outboundSelect.appendChild(optionWarp);
      }

      if (rule) {
        title.textContent = veilT('routing.edit');
        nameInput.value = rule.name || '';
        nameInput.readOnly = true;
        matchInput.value = rule.match || '';

        if (rule.outbound === 'warp' && !warpEnabled) {
          const optionWarp = document.createElement('option');
          optionWarp.value = 'warp';
          optionWarp.textContent = 'warp';
          outboundSelect.appendChild(optionWarp);
        }
        outboundSelect.value = rule.outbound || 'direct';
        enabledCheckbox.checked = Boolean(rule.enabled);
        deleteBtn.style.display = 'inline-block';
      } else {
        title.textContent = veilT('routing.add');
        nameInput.value = '';
        nameInput.readOnly = false;
        matchInput.value = '';
        outboundSelect.value = 'direct';
        enabledCheckbox.checked = true;
        deleteBtn.style.display = 'none';
      }

      openVeilDialog(modal);
    }

    function closeRoutingModal() {
      routingModalGeneration += 1;
      const modal = document.getElementById('routing-modal');
      if (modal) closeVeilDialog(modal);
    }

    function appendRoutingEmptyState(tbody) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 5;
      cell.className = 'empty-state';
      cell.textContent = 'No routing rules found. Click "Add Rule" to create one.';
      row.appendChild(cell);
      tbody.appendChild(row);
    }

    function renderRoutingRulesTable(rules) {
      const tbody = document.getElementById('routing-rules-tbody');
      if (!tbody) return;
      tbody.textContent = '';
      if (!Array.isArray(rules) || rules.length === 0) {
        appendRoutingEmptyState(tbody);
        return;
      }
      rules.forEach((rule) => {
        const tr = document.createElement('tr');

        const tdName = document.createElement('td');
        tdName.className = 'font-semibold';
        tdName.textContent = rule.name || '';

        const tdMatch = document.createElement('td');
        const codeMatch = document.createElement('code');
        codeMatch.className = 'match-badge';
        codeMatch.textContent = rule.match || '';
        tdMatch.appendChild(codeMatch);

        const tdOutbound = document.createElement('td');
        const spanOutbound = document.createElement('span');
        const outbound = rule.outbound || 'direct';
        spanOutbound.className = 'outbound-badge badge-' + outbound;
        spanOutbound.textContent = outbound;
        tdOutbound.appendChild(spanOutbound);

        const tdStatus = document.createElement('td');
        const labelSwitch = document.createElement('label');
        labelSwitch.className = 'switch small-switch';
        const inputSwitch = document.createElement('input');
        inputSwitch.type = 'checkbox';
        inputSwitch.dataset.adminOnly = 'true';
        inputSwitch.dataset.routingMutation = 'true';
        inputSwitch.checked = Boolean(rule.enabled);
        inputSwitch.addEventListener('change', async () => {
          const requestedState = inputSwitch.checked;
          const payload = {
            name: rule.name,
            match: rule.match,
            outbound: rule.outbound,
            enabled: requestedState
          };
          const updated = await withRoutingMutation(() => loadJSON('/api/routing/rules/' + encodeURIComponent(rule.name), 'routing-output', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
          }));
          if (!updated) {
            inputSwitch.checked = !requestedState;
            inputSwitch.disabled = isViewerRole();
            return;
          }
          await loadRoutingRules();
        });
        const spanSlider = document.createElement('span');
        spanSlider.className = 'slider round';
        labelSwitch.appendChild(inputSwitch);
        labelSwitch.appendChild(spanSlider);
        tdStatus.appendChild(labelSwitch);

        const tdActions = document.createElement('td');
        const btnEdit = document.createElement('button');
        btnEdit.type = 'button';
        btnEdit.className = 'btn-action-edit';
        btnEdit.dataset.adminOnly = 'true';
        btnEdit.dataset.routingMutation = 'true';
        btnEdit.textContent = veilT('users.edit');
        btnEdit.addEventListener('click', () => openRoutingModal(rule));

        tdActions.appendChild(btnEdit);
        tr.appendChild(tdName);
        tr.appendChild(tdMatch);
        tr.appendChild(tdOutbound);
        tr.appendChild(tdStatus);
        tr.appendChild(tdActions);
        tbody.appendChild(tr);
      });
      applyViewerRoleGuard();
    }

    async function loadRoutingRules() {
      if (routingMutationInFlight) return null;
      const generation = ++routingRulesLoadGeneration;
      if (routingRulesLoadController) routingRulesLoadController.abort();
      const controller = new AbortController();
      routingRulesLoadController = controller;
      const output = document.getElementById('routing-output');
      const loadButton = document.getElementById('load-routing-rules');
      if (loadButton) loadButton.disabled = true;
      if (output) output.textContent = veilT('status.loadingPath', { path: '/api/routing/rules' });
      try {
        const response = await fetch('/api/routing/rules', {
          headers: authHeaders(),
          signal: controller.signal
        });
        const text = await response.text();
        if (generation !== routingRulesLoadGeneration || controller.signal.aborted) return null;
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const rules = text ? JSON.parse(text) : [];
        if (!Array.isArray(rules)) throw new Error('Invalid routing rules response.');
        if (output) output.textContent = JSON.stringify(rules, null, 2);
        renderRoutingRulesTable(rules);
        return rules;
      } catch (error) {
        if (error && error.name === 'AbortError') return null;
        if (generation !== routingRulesLoadGeneration) return null;
        if (output) {
          output.textContent = veilT('status.requestFailed', { error: String(error && error.message ? error.message : error) });
        }
        return null;
      } finally {
        if (routingRulesLoadController === controller) routingRulesLoadController = null;
        if (loadButton && generation === routingRulesLoadGeneration) loadButton.disabled = routingMutationInFlight;
      }
    }

    async function saveRoutingRule(event) {
      event.preventDefault();
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = veilT('routing.nameRequired');
        return;
      }
      const payload = {
        name: name,
        match: document.getElementById('routing-rule-match').value,
        outbound: document.getElementById('routing-rule-outbound').value,
        enabled: document.getElementById('routing-rule-enabled').checked
      };
      const isEdit = document.getElementById('routing-rule-name').readOnly;
      const saved = await withRoutingMutation(() => loadJSON(isEdit ? '/api/routing/rules/' + encodeURIComponent(name) : '/api/routing/rules', 'routing-output', {
        method: isEdit ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      }));
      if (!saved) return;
      closeRoutingModal();
      await loadRoutingRules();
    }

    async function deleteRoutingRule() {
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = veilT('routing.nameRequired');
        return;
      }
      if (!confirm(veilT('confirm.deleteRoutingRule', { name }))) return;
      const deleted = await withRoutingMutation(() => loadJSON('/api/routing/rules/' + encodeURIComponent(name), 'routing-output', { method: 'DELETE' }));
      if (!deleted) return;
      closeRoutingModal();
      await loadRoutingRules();
    }

    async function applyRoutingPreset() {
      const profile = document.getElementById('routing-preset-profile').value;
      const applied = await withRoutingMutation(() => loadJSON('/api/routing/presets/' + encodeURIComponent(profile), 'routing-output', { method: 'POST' }));
      if (!applied) return;
      await loadRoutingRules();
    }`
}
