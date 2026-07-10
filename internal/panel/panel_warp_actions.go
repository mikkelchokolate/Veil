package panel

const panelWarpActionsPlaceholder = "__VEIL_PANEL_WARP_ACTIONS__"

func panelWarpActionsJS() string {
	return `    function fillWarpForm(data) {
      if (!data) {
        return;
      }
      document.getElementById('warp-enabled').checked = Boolean(data.enabled);
      document.getElementById('warp-endpoint').value = data.endpoint || '';
      document.getElementById('warp-local-address').value = data.localAddress || '';
      document.getElementById('warp-peer-public-key').value = data.peerPublicKey || '';
      document.getElementById('warp-private-key').value = data.privateKey || '';
      document.getElementById('warp-license-key').value = data.licenseKey || '';
      document.getElementById('warp-reserved').value = Array.isArray(data.reserved) ? data.reserved.join(',') : '';
      document.getElementById('warp-socks-listen').value = data.socksListen || '';
      document.getElementById('warp-socks-port').value = data.socksPort || '';
      document.getElementById('warp-mtu').value = data.mtu || '';
    }

    async function loadWarpIntoForm() {
      const data = await loadJSON('/api/warp', 'warp-output');
      fillWarpForm(data);
    }

    function warpFormPayload(enabled) {
      return {
        enabled: enabled,
        licenseKey: document.getElementById('warp-license-key').value,
        endpoint: document.getElementById('warp-endpoint').value,
        privateKey: document.getElementById('warp-private-key').value,
        localAddress: document.getElementById('warp-local-address').value,
        peerPublicKey: document.getElementById('warp-peer-public-key').value,
        reserved: parseReserved(document.getElementById('warp-reserved').value),
        socksListen: document.getElementById('warp-socks-listen').value,
        socksPort: numberOrZero('warp-socks-port'),
        mtu: numberOrZero('warp-mtu')
      };
    }

    // commitWarp persists the WARP config and then applies it so the change
    // takes effect on the running system. It reports save and apply outcomes
    // separately because an apply failure must not make the UI invert a config
    // that was already saved successfully.
    let warpCommitInFlight = false;
    async function commitWarp(enabled) {
      if (warpCommitInFlight) {
        return null;
      }
      warpCommitInFlight = true;
      const toggle = document.getElementById('warp-enabled');
      const saveBtn = document.getElementById('save-warp-config');
      const output = document.getElementById('warp-output');
      if (toggle) { toggle.disabled = true; }
      if (saveBtn) { saveBtn.disabled = true; }
      try {
        const payload = warpFormPayload(enabled);
        const saved = await loadJSON('/api/warp', 'warp-output', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (!saved) {
          return { saved: null, applied: false };
        }
        fillWarpForm(saved);
        // Promote staged configs and reload services so WARP actually turns
        // on/off now, rather than only after a separate manual apply.
        const applied = await loadJSON('/api/apply', 'warp-output', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true, applyLive: true, applyServices: true })
        });
        return { saved, applied: applied !== null };
      } catch (err) {
        if (output) output.textContent = veilT('status.requestFailed', { error: String(err) });
        return { saved: null, applied: false };
      } finally {
        warpCommitInFlight = false;
        if (toggle) { toggle.disabled = isViewerRole(); }
        if (saveBtn) { saveBtn.disabled = isViewerRole(); }
        applyViewerRoleGuard();
      }
    }

    async function applyWarpToggle() {
      const toggle = document.getElementById('warp-enabled');
      const enabled = toggle.checked;
      const result = await commitWarp(enabled);
      if (!result || !result.saved) {
        // Save was rejected: snap the slider back to its previous state.
        toggle.checked = !enabled;
        return;
      }
      // The saved state remains authoritative even if the subsequent live apply
      // failed. loadJSON keeps the apply error visible for the operator.
      toggle.checked = Boolean(result.saved.enabled);
    }

    async function saveWarpConfig(event) {
      event.preventDefault();
      await commitWarp(document.getElementById('warp-enabled').checked);
    }

    // Auto-load WARP config on page mount
    setTimeout(loadWarpIntoForm, 150);`
}
