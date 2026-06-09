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
    // takes effect on the running system. The slider toggle is a one-click
    // on/off control: flipping it (or pressing Save) must both save the
    // desired state (which adds/removes the WARP routing rule) and apply it
    // (which starts/stops the sing-box WARP service). Returns the saved config
    // on success, or null if the save was rejected.
    let warpCommitInFlight = false;
    async function commitWarp(enabled) {
      if (warpCommitInFlight) {
        return null;
      }
      warpCommitInFlight = true;
      const toggle = document.getElementById('warp-enabled');
      const saveBtn = document.getElementById('save-warp-config');
      if (toggle) { toggle.disabled = true; }
      if (saveBtn) { saveBtn.disabled = true; }
      try {
        const saved = await loadJSON('/api/warp', 'warp-output', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(warpFormPayload(enabled))
        });
        if (!saved) {
          return null;
        }
        fillWarpForm(saved);
        // Promote staged configs and reload services so WARP actually turns
        // on/off now, rather than only after a separate manual apply.
        await loadJSON('/api/apply', 'warp-output', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true, applyLive: true, applyServices: true })
        });
        return saved;
      } finally {
        if (toggle) { toggle.disabled = false; }
        if (saveBtn) { saveBtn.disabled = false; }
        warpCommitInFlight = false;
      }
    }

    async function applyWarpToggle() {
      const toggle = document.getElementById('warp-enabled');
      const enabled = toggle.checked;
      const saved = await commitWarp(enabled);
      if (!saved) {
        // Save was rejected: snap the slider back to its real state and keep
        // the error visible in the console.
        toggle.checked = !enabled;
      }
    }

    async function saveWarpConfig(event) {
      event.preventDefault();
      await commitWarp(document.getElementById('warp-enabled').checked);
    }

    // Auto-load WARP config on page mount
    setTimeout(loadWarpIntoForm, 150);`
}
