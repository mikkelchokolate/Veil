package api

const panelRoutingActionsPlaceholder = "__VEIL_PANEL_ROUTING_ACTIONS__"

func panelRoutingActionsJS() string {
	return `    async function saveRoutingRule(event) {
      event.preventDefault();
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = 'Routing rule name is required';
        return;
      }
      const payload = {
        name: name,
        match: document.getElementById('routing-rule-match').value,
        outbound: document.getElementById('routing-rule-outbound').value,
        enabled: document.getElementById('routing-rule-enabled').checked
      };
      const rules = await loadJSON('/api/routing/rules', 'routing-output');
      const exists = Array.isArray(rules) && rules.some((rule) => rule.name === name);
      await loadJSON(exists ? '/api/routing/rules/' + encodeURIComponent(name) : '/api/routing/rules', 'routing-output', {
        method: exists ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
    }

    async function deleteRoutingRule() {
      const name = document.getElementById('routing-rule-name').value.trim();
      if (!name) {
        document.getElementById('routing-output').textContent = 'Routing rule name is required';
        return;
      }
      await loadJSON('/api/routing/rules/' + encodeURIComponent(name), 'routing-output', { method: 'DELETE' });
    }

    async function applyRoutingPreset() {
      const profile = document.getElementById('routing-preset-profile').value;
      await loadJSON('/api/routing/presets/' + encodeURIComponent(profile), 'routing-output', { method: 'POST' });
    }`
}
