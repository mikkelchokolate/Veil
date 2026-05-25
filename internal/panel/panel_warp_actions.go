package panel

const panelWarpActionsPlaceholder = "__VEIL_PANEL_WARP_ACTIONS__"

func panelWarpActionsJS() string {
	return `    async function loadWarpIntoForm() {
      const data = await loadJSON('/api/warp', 'warp-output');
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

    async function saveWarpConfig(event) {
      event.preventDefault();
      await loadJSON('/api/warp', 'warp-output', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: document.getElementById('warp-enabled').checked,
          licenseKey: document.getElementById('warp-license-key').value,
          endpoint: document.getElementById('warp-endpoint').value,
          privateKey: document.getElementById('warp-private-key').value,
          localAddress: document.getElementById('warp-local-address').value,
          peerPublicKey: document.getElementById('warp-peer-public-key').value,
          reserved: parseReserved(document.getElementById('warp-reserved').value),
          socksListen: document.getElementById('warp-socks-listen').value,
          socksPort: numberOrZero('warp-socks-port'),
          mtu: numberOrZero('warp-mtu')
        })
      });
    }

    // Auto-load WARP config on page mount
    setTimeout(loadWarpIntoForm, 150);`
}
