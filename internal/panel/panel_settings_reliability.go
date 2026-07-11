package panel

func panelSettingsReliabilityJS() string {
	return `
    let settingsLoadGeneration = 0;
    let settingsSaveInFlight = false;

    function setSettingsControlsDisabled(disabled) {
      const saveButton = document.getElementById('save-settings');
      if (saveButton) saveButton.disabled = Boolean(disabled) || isViewerRole();
      const loadButton = document.getElementById('load-settings');
      if (loadButton) loadButton.disabled = Boolean(disabled);
    }

    async function applySettingsData(data, generation) {
      if (generation !== settingsLoadGeneration) return false;
      window.cachedSettings = data;
      document.getElementById('settings-panel-listen').value = data.panelListen || '';
      document.getElementById('settings-mode').value = data.mode || '';
      document.getElementById('settings-panel-access').value = data.panelAccess || 'local';
      document.getElementById('settings-web-base-path').value = data.webBasePath || '';
      document.getElementById('settings-domain').value = data.domain || '';
      document.getElementById('settings-email').value = data.email || '';
      try {
        await ensureProtocolSchemas();
        if (generation !== settingsLoadGeneration) return false;
        const values = Object.assign({}, data, data.protocolFields || {});
        renderSettingsProtocolFields(values);
        return true;
      } catch (error) {
        if (generation !== settingsLoadGeneration) return false;
        const container = document.getElementById('settings-protocol-fields');
        if (container) container.textContent = 'Could not load protocol settings: ' + String(error && error.message ? error.message : error);
        return false;
      }
    }

    loadSettingsIntoForm = async function() {
      if (!document.getElementById('settings-form')) return null;
      const generation = ++settingsLoadGeneration;
      const output = document.getElementById('settings-output');
      const loadButton = document.getElementById('load-settings');
      if (loadButton) loadButton.disabled = true;
      if (output) output.textContent = veilT('status.loadingPath', { path: '/api/settings' });
      try {
        const response = await fetch('/api/settings', { headers: authHeaders() });
        const text = await response.text();
        if (generation !== settingsLoadGeneration) return null;
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const data = text ? JSON.parse(text) : {};
        if (!data || typeof data !== 'object' || Array.isArray(data)) {
          throw new Error('Invalid settings response.');
        }
        if (output) output.textContent = JSON.stringify(data, null, 2);
        await applySettingsData(data, generation);
        return data;
      } catch (error) {
        if (generation === settingsLoadGeneration && output) {
          output.textContent = veilT('status.requestFailed', { error: String(error && error.message ? error.message : error) });
        }
        return null;
      } finally {
        if (loadButton && generation === settingsLoadGeneration) loadButton.disabled = false;
      }
    };

    saveSettings = async function(event) {
      if (event) event.preventDefault();
      const form = document.getElementById('settings-form');
      if (!form || settingsSaveInFlight) return null;
      if (!form.checkValidity()) {
        form.reportValidity();
        return null;
      }
      settingsSaveInFlight = true;
      const generation = ++settingsLoadGeneration;
      setSettingsControlsDisabled(true);
      const output = document.getElementById('settings-output');
      const payload = {
        panelListen: document.getElementById('settings-panel-listen').value.trim(),
        panelAccess: document.getElementById('settings-panel-access').value,
        webBasePath: document.getElementById('settings-web-base-path').value.trim(),
        mode: document.getElementById('settings-mode').value.trim(),
        domain: document.getElementById('settings-domain').value.trim(),
        email: document.getElementById('settings-email').value.trim(),
        protocolFields: collectSettingsProtocolFields()
      };
      if (output) output.textContent = veilT('status.loadingPath', { path: '/api/settings' });
      try {
        const response = await fetch('/api/settings', {
          method: 'PUT',
          headers: requestHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify(payload)
        });
        const text = await response.text();
        if (!response.ok) {
          if (output) output.textContent = formatAPIError(text, response.status);
          return null;
        }
        const saved = text ? JSON.parse(text) : {};
        if (!saved || typeof saved !== 'object' || Array.isArray(saved)) {
          throw new Error('Invalid settings response.');
        }
        if (output) output.textContent = JSON.stringify(saved, null, 2);
        notifyPanelConfigurationChanged('/api/settings');
        await applySettingsData(saved, generation);
        return saved;
      } catch (error) {
        if (output) output.textContent = veilT('status.requestFailed', { error: String(error && error.message ? error.message : error) });
        return null;
      } finally {
        settingsSaveInFlight = false;
        setSettingsControlsDisabled(false);
        applyViewerRoleGuard();
      }
    };
`
}
