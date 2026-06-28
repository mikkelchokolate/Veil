package panel

const panelClientLinksActionsPlaceholder = "__VEIL_PANEL_CLIENT_LINKS_ACTIONS__"

func panelClientLinksActionsJS() string {
	return `    async function loadClientLinks() {
      await loadJSON('/api/client-links', 'client-links-output');
    }

    async function loadClientSubscription() {
      await loadClientSubscriptionPath('/api/client-links/subscription?format=base64');
    }

    async function loadRawClientSubscription() {
      await loadClientSubscriptionPath('/api/client-links/subscription?format=raw');
    }

    async function loadClientSubscriptionPath(path) {
      const output = document.getElementById('client-links-output');
      output.textContent = veilT('status.loadingPath', { path });
      try {
        const response = await fetch(path, { headers: requestHeaders() });
        const text = await response.text();
        output.textContent = response.ok ? text : (text || ('HTTP ' + response.status));
      } catch (err) {
        output.textContent = String(err);
      }
    }

    async function downloadMieruConfigs() {
      const output = document.getElementById('client-links-output');
      output.textContent = veilT('clientLinks.loadingMieru');
      try {
        const response = await fetch('/api/client-links', { headers: requestHeaders() });
        const body = await response.json();
        if (!response.ok) {
          output.textContent = JSON.stringify(body, null, 2);
          return;
        }
        const configs = (body.artifacts || []).filter(artifact => artifact.protocol === 'mieru' && artifact.content).map(artifact => ({ name: artifact.name, config: JSON.parse(artifact.content) }));
        if (!configs.length) {
          output.textContent = veilT('clientLinks.noMieru');
          return;
        }
        const blob = new Blob([JSON.stringify(configs, null, 2) + '\n'], { type: 'application/json;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = 'mieru-client-configs.json';
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        output.textContent = veilT('clientLinks.downloaded', { filename: 'mieru-client-configs.json' });
      } catch (err) {
        output.textContent = veilT('clientLinks.mieruFailed', { error: String(err) });
      }
    }

    async function downloadClientLinksJSON() {
      const output = document.getElementById('client-links-output');
      output.textContent = veilT('clientLinks.downloadingJSON');
      try {
        const response = await fetch('/api/client-links', { headers: requestHeaders() });
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return;
        }
        const body = JSON.parse(text);
        const blob = new Blob([JSON.stringify(body, null, 2) + '\n'], { type: 'application/json;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = 'veil-client-links.json';
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        output.textContent = veilT('clientLinks.downloaded', { filename: 'veil-client-links.json' });
      } catch (err) {
        output.textContent = veilT('status.downloadFailed', { error: String(err) });
      }
    }

    async function copyClientLinksOutput() {
      const output = document.getElementById('client-links-output');
      const text = output.textContent || '';
      if (!text || text === veilT('status.notLoaded')) {
        output.textContent = veilT('clientLinks.nothingToCopy');
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        output.textContent = text + '\n\n' + veilT('clientLinks.copiedOutput');
      } catch (err) {
        output.textContent = text + '\n\n' + veilT('clientLinks.copyFailed', { error: String(err) });
      }
    }

    async function downloadClientSubscriptionPath(path, filename) {
      const output = document.getElementById('client-links-output');
      output.textContent = veilT('clientLinks.downloading', { path });
      try {
        const response = await fetch(path, { headers: requestHeaders() });
        const text = await response.text();
        if (!response.ok) {
          output.textContent = text || ('HTTP ' + response.status);
          return;
        }
        const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        output.textContent = veilT('clientLinks.downloaded', { filename });
      } catch (err) {
        output.textContent = veilT('status.downloadFailed', { error: String(err) });
      }
    }

    // Modal helpers for Inbound actions list
    window.openClientLinksModal = async function() {
      await window.openClientLinksModalFor('');
    };

    window.openClientLinksModalFor = async function(inboundName, inboundProtocol) {
      document.getElementById('client-links-modal-title').innerText = inboundName
        ? veilT('clientLinks.inboundTitle', { name: inboundName })
        : veilT('clientLinks.connectionTitle');
      const container = document.getElementById('modal-links-container');
      container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;"></div>';
      container.firstElementChild.textContent = veilT('clientLinks.loading');
      openVeilDialog(document.getElementById('client-links-modal-overlay'));

      fetch('/api/client-links', { headers: requestHeaders() })
      .then(res => res.json())
      .then(body => {
        if (body.message || body.error) {
          container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;"></div>';
          container.firstElementChild.textContent = body.message || veilT('clientLinks.failed');
          return;
        }

        let links = body.links || [];
        if (inboundName) {
          links = links.filter((link) => {
            if (link.name === inboundName) return true;
            if (link.name.indexOf(inboundName + '/') === 0) return true;
            // Aggregate links (e.g. Mieru) are named "protocol/profile"; match by protocol.
            if (inboundProtocol && link.name.indexOf(inboundProtocol + '/') === 0) return true;
            return false;
          });
        }
        if (links.length === 0) {
          container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;"></div>';
          container.firstElementChild.textContent = veilT('clientLinks.empty');
          return;
        }

        container.innerHTML = '';
        links.forEach((link, idx) => {
          container.appendChild(renderClientLinkModalItem(link, idx));
        });
      })
      .catch(err => {
        container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;"></div>';
        container.firstElementChild.textContent = veilT('common.error', { error: String(err) });
      });
    };

    window.renderClientLinkModalItem = function(link, idx) {
      const item = document.createElement('div');
      item.style.padding = '12px';
      item.style.marginBottom = '12px';
      item.style.border = '1px solid var(--border)';
      item.style.borderRadius = '0';
      item.style.background = 'var(--surface)';

      const header = document.createElement('div');
      header.style.display = 'flex';
      header.style.justifyContent = 'space-between';
      header.style.alignItems = 'center';
      header.style.marginBottom = '8px';

      const name = document.createElement('span');
      name.style.fontWeight = '600';
      name.style.color = '#fff';
      name.textContent = link.name + ' ';
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.style.marginLeft = '8px';
      badge.textContent = link.protocol;
      name.appendChild(badge);
      header.appendChild(name);

      const meta = document.createElement('span');
      meta.style.fontSize = '0.8rem';
      meta.style.color = 'var(--text-muted)';
      meta.style.textTransform = 'uppercase';
      meta.textContent = link.transport + ' / ' + veilT('clientLinks.port', { port: link.port });
      header.appendChild(meta);
      item.appendChild(header);

      const row = document.createElement('div');
      row.style.display = 'flex';
      row.style.gap = '8px';
      row.style.marginBottom = '12px';
      const uniqueId = 'link-input-' + idx;
      const input = document.createElement('input');
      input.id = uniqueId;
      input.type = 'text';
      input.readOnly = true;
      input.value = link.uri;
      input.style.flex = '1';
      input.style.fontFamily = 'monospace';
      input.style.fontSize = '0.82rem';
      row.appendChild(input);
      const copyBtn = document.createElement('button');
      copyBtn.type = 'button';
      copyBtn.className = 'secondary';
      copyBtn.textContent = veilT('action.copy');
      copyBtn.addEventListener('click', () => copyModalLink(uniqueId, copyBtn));
      row.appendChild(copyBtn);
      item.appendChild(row);

      const actions = document.createElement('div');
      actions.style.display = 'flex';
      actions.style.justifyContent = 'flex-end';
      actions.style.gap = '8px';
      const downloadBtn = document.createElement('button');
      downloadBtn.type = 'button';
      downloadBtn.className = 'secondary';
      downloadBtn.textContent = veilT('action.download');
      downloadBtn.addEventListener('click', () => downloadSingleClientLink(link));
      actions.appendChild(downloadBtn);
      const qrBtn = document.createElement('button');
      qrBtn.type = 'button';
      qrBtn.className = 'secondary';
      qrBtn.textContent = veilT('clientLinks.showQR');
      const qrId = 'link-qr-' + idx;
      qrBtn.addEventListener('click', () => toggleQR(qrId, link.uri));
      actions.appendChild(qrBtn);
      item.appendChild(actions);

      const qr = document.createElement('div');
      qr.id = qrId;
      qr.style.display = 'none';
      qr.style.justifyContent = 'center';
      qr.style.marginTop = '16px';
      qr.style.padding = '12px';
      qr.style.background = '#fff';
      qr.style.width = '180px';
      qr.style.height = '180px';
      qr.style.marginLeft = 'auto';
      qr.style.marginRight = 'auto';
      item.appendChild(qr);
      return item;
    };

    window.closeClientLinksModal = function() {
      closeVeilDialog(document.getElementById('client-links-modal-overlay'));
    };

    window.copyModalLink = function(inputId, btn) {
      const input = document.getElementById(inputId);
      input.select();
      input.setSelectionRange(0, 99999);
      try {
        navigator.clipboard.writeText(input.value).then(() => {
          const origText = btn.innerText;
          btn.innerText = veilT('clientLinks.copied');
          btn.style.background = 'var(--accent-success)';
          btn.style.color = '#fff';
          setTimeout(() => {
            btn.textContent = origText;
            btn.style.background = '';
            btn.style.color = '';
          }, 1500);
        });
      } catch (err) {
        alert(veilT('clientLinks.copyFailedAlert', { error: String(err) }));
      }
    };

    window.downloadSingleClientLink = function(link) {
      const body = JSON.stringify({ name: link.name, protocol: link.protocol, transport: link.transport, port: link.port, uri: link.uri }, null, 2) + '\n';
      const blob = new Blob([body], { type: 'application/json;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = 'veil-client-' + String(link.name || 'link').replace(/[^a-z0-9._-]+/gi, '-') + '.json';
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    };

    window.toggleQR = async function(qrId, uri) {
      const container = document.getElementById(qrId);
      if (container.style.display === 'none' || container.style.display === '') {
        container.textContent = veilT('clientLinks.renderingQR');
        container.style.display = 'flex';
        try {
          const response = await fetch('/api/client-links/qr', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ uri })
          });
          if (!response.ok) {
            container.textContent = await response.text();
            return;
          }
          const blob = await response.blob();
          if (container.dataset.objectUrl) {
            URL.revokeObjectURL(container.dataset.objectUrl);
          }
          const url = URL.createObjectURL(blob);
          container.dataset.objectUrl = url;
          container.innerHTML = '';
          const image = document.createElement('img');
          image.src = url;
          image.alt = veilT('clientLinks.qrAlt');
          image.style.width = '100%';
          image.style.height = '100%';
          image.style.border = '0';
          container.appendChild(image);
        } catch (err) {
          container.textContent = veilT('clientLinks.qrFailed', { error: String(err) });
        }
      } else {
        if (container.dataset.objectUrl) {
          URL.revokeObjectURL(container.dataset.objectUrl);
          delete container.dataset.objectUrl;
        }
        container.style.display = 'none';
        container.innerHTML = '';
      }
    };`
}
