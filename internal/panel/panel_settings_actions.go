package panel

const panelSettingsActionsPlaceholder = "__VEIL_PANEL_SETTINGS_ACTIONS__"

func panelSettingsActionsJS() string {
	return `    async function loadSettingsIntoForm() {
      const data = await loadJSON('/api/settings', 'settings-output');
      if (!data) {
        return;
      }
      document.getElementById('settings-panel-listen').value = data.panelListen || '';
      document.getElementById('settings-mode').value = data.mode || '';
      document.getElementById('settings-panel-access').value = data.panelAccess || 'local';
      document.getElementById('settings-web-base-path').value = data.webBasePath || '';
      document.getElementById('settings-domain').value = data.domain || '';
      document.getElementById('settings-email').value = data.email || '';
      document.getElementById('settings-naive-username').value = data.naiveUsername || '';
      document.getElementById('settings-naive-password').value = data.naivePassword || '';
      document.getElementById('settings-hysteria2-password').value = data.hysteria2Password || '';
      document.getElementById('settings-masquerade-url').value = data.masqueradeURL || '';
      document.getElementById('settings-fallback-root').value = data.fallbackRoot || '';
    }

    async function saveSettings(event) {
      event.preventDefault();
      await loadJSON('/api/settings', 'settings-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          panelListen: document.getElementById('settings-panel-listen').value,
          panelAccess: document.getElementById('settings-panel-access').value,
          webBasePath: document.getElementById('settings-web-base-path').value,
          mode: document.getElementById('settings-mode').value,
          domain: document.getElementById('settings-domain').value,
          email: document.getElementById('settings-email').value,
          naiveUsername: document.getElementById('settings-naive-username').value,
          naivePassword: document.getElementById('settings-naive-password').value,
          hysteria2Password: document.getElementById('settings-hysteria2-password').value,
          masqueradeURL: document.getElementById('settings-masquerade-url').value,
          fallbackRoot: document.getElementById('settings-fallback-root').value
        })
      });
    }`
}
