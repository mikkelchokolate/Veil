package panel

const panelRoutingActionsPlaceholder = "__VEIL_PANEL_ROUTING_ACTIONS__"

func panelRoutingActionsJS() string {
	return `    async function openRoutingModal(rule) {
      const modal = document.getElementById('routing-modal');
      const title = document.getElementById('modal-title');
      const nameInput = document.getElementById('routing-rule-name');
      const matchInput = document.getElementById('routing-rule-match');
      const outboundSelect = document.getElementById('routing-rule-outbound');
      const enabledCheckbox = document.getElementById('routing-rule-enabled');
      const deleteBtn = document.getElementById('delete-routing-rule');
      
      if (!modal) return;

      // Query WARP status dynamically to decide if we add "warp"
      let warpEnabled = false;
      try {
        const response = await fetch('/api/warp');
        if (response.ok) {
          const warpData = await response.json();
          warpEnabled = Boolean(warpData.enabled);
        }
      } catch (err) {
        console.error('Failed to query WARP status:', err);
      }

      // Rebuild options in outboundSelect
      outboundSelect.innerHTML = '';
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
        
        // Ensure "warp" option exists if the rule already uses it, even if currently disabled
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
      
      modal.classList.add('active');
    }
    
    function closeRoutingModal() {
      const modal = document.getElementById('routing-modal');
      if (modal) {
        modal.classList.remove('active');
      }
    }

    function renderRoutingRulesTable(rules) {
      const tbody = document.getElementById('routing-rules-tbody');
      if (!tbody) return;
      if (!Array.isArray(rules) || rules.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No routing rules found. Click "Add Rule" to create one.</td></tr>';
        return;
      }
      tbody.innerHTML = '';
      rules.forEach((rule) => {
        const tr = document.createElement('tr');
        
        // Name Cell
        const tdName = document.createElement('td');
        tdName.className = 'font-semibold';
        tdName.textContent = rule.name || '';
        
        // Match Cell
        const tdMatch = document.createElement('td');
        const codeMatch = document.createElement('code');
        codeMatch.className = 'match-badge';
        codeMatch.textContent = rule.match || '';
        tdMatch.appendChild(codeMatch);
        
        // Outbound Cell
        const tdOutbound = document.createElement('td');
        const spanOutbound = document.createElement('span');
        const outbound = rule.outbound || 'direct';
        spanOutbound.className = 'outbound-badge badge-' + outbound;
        spanOutbound.textContent = outbound;
        tdOutbound.appendChild(spanOutbound);
        
        // Status Cell with switch toggle
        const tdStatus = document.createElement('td');
        const labelSwitch = document.createElement('label');
        labelSwitch.className = 'switch small-switch';
        const inputSwitch = document.createElement('input');
        inputSwitch.type = 'checkbox';
        inputSwitch.dataset.adminOnly = 'true';
        inputSwitch.checked = Boolean(rule.enabled);
        inputSwitch.addEventListener('change', async () => {
          const payload = {
            name: rule.name,
            match: rule.match,
            outbound: rule.outbound,
            enabled: inputSwitch.checked
          };
          await loadJSON('/api/routing/rules/' + encodeURIComponent(rule.name), 'routing-output', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
          });
          loadRoutingRules();
        });
        const spanSlider = document.createElement('span');
        spanSlider.className = 'slider round';
        labelSwitch.appendChild(inputSwitch);
        labelSwitch.appendChild(spanSlider);
        tdStatus.appendChild(labelSwitch);
        
        // Actions Cell
        const tdActions = document.createElement('td');
        const btnEdit = document.createElement('button');
        btnEdit.type = 'button';
        btnEdit.className = 'btn-action-edit';
        btnEdit.dataset.adminOnly = 'true';
        btnEdit.textContent = veilT('users.edit');
        btnEdit.addEventListener('click', () => {
          openRoutingModal(rule);
        });
        
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
      const rules = await loadJSON('/api/routing/rules', 'routing-output');
      renderRoutingRulesTable(rules);
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
      
      await loadJSON(isEdit ? '/api/routing/rules/' + encodeURIComponent(name) : '/api/routing/rules', 'routing-output', {
        method: isEdit ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      
      closeRoutingModal();
      loadRoutingRules();
    }

    async function deleteRoutingRule() {
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = veilT('routing.nameRequired');
        return;
      }
      if (confirm(veilT('confirm.deleteRoutingRule', { name }))) {
        await loadJSON('/api/routing/rules/' + encodeURIComponent(name), 'routing-output', { method: 'DELETE' });
        closeRoutingModal();
        loadRoutingRules();
      }
    }

    async function applyRoutingPreset() {
      const profile = document.getElementById('routing-preset-profile').value;
      await loadJSON('/api/routing/presets/' + encodeURIComponent(profile), 'routing-output', { method: 'POST' });
      setTimeout(loadRoutingRules, 800);
    }

    // Auto-load routing rules on page mount
    setTimeout(loadRoutingRules, 100);`
}
