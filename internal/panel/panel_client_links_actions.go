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
      output.textContent = 'Loading ' + path + '...';
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
      output.textContent = 'Loading Mieru client configs...';
      try {
        const response = await fetch('/api/client-links', { headers: requestHeaders() });
        const body = await response.json();
        if (!response.ok) {
          output.textContent = JSON.stringify(body, null, 2);
          return;
        }
        const configs = (body.artifacts || []).filter(artifact => artifact.protocol === 'mieru' && artifact.content).map(artifact => ({ name: artifact.name, config: JSON.parse(artifact.content) }));
        if (!configs.length) {
          output.textContent = 'No Mieru client configs available';
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
        output.textContent = 'Downloaded mieru-client-configs.json';
      } catch (err) {
        output.textContent = 'Mieru config download failed: ' + String(err);
      }
    }

    async function downloadClientLinksJSON() {
      const output = document.getElementById('client-links-output');
      output.textContent = 'Downloading client links JSON...';
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
        output.textContent = 'Downloaded veil-client-links.json';
      } catch (err) {
        output.textContent = 'Download failed: ' + String(err);
      }
    }

    async function copyClientLinksOutput() {
      const output = document.getElementById('client-links-output');
      const text = output.textContent || '';
      if (!text || text === 'Not loaded') {
        output.textContent = 'Nothing to copy yet';
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        output.textContent = text + '\n\nCopied to clipboard';
      } catch (err) {
        output.textContent = text + '\n\nCopy failed: ' + String(err);
      }
    }

    async function downloadClientSubscriptionPath(path, filename) {
      const output = document.getElementById('client-links-output');
      output.textContent = 'Downloading ' + path + '...';
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
        output.textContent = 'Downloaded ' + filename;
      } catch (err) {
        output.textContent = 'Download failed: ' + String(err);
      }
    }

    // Modal helpers for Inbound actions list
    window.openClientLinksModal = async function() {
      await window.openClientLinksModalFor('');
    };

    window.openClientLinksModalFor = async function(inboundName) {
      document.getElementById('client-links-modal-title').innerText = inboundName ? ('Client Links: ' + inboundName) : 'Client Connection Links';
      const container = document.getElementById('modal-links-container');
      container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;">Loading connection links...</div>';
      document.getElementById('client-links-modal-overlay').classList.add('active');

      fetch('/api/client-links', { headers: requestHeaders() })
      .then(res => res.json())
      .then(body => {
        if (body.message || body.error) {
          container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;">' + (body.message || 'Failed to load links') + '</div>';
          return;
        }

        let links = body.links || [];
        if (inboundName) {
          links = links.filter((link) => link.name === inboundName || link.name.indexOf(inboundName + '/') === 0);
        }
        if (links.length === 0) {
          container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;">No connection links available for this inbound. Make sure the inbound is enabled and global settings are saved.</div>';
          return;
        }

        container.innerHTML = '';
        links.forEach((link, idx) => {
          container.appendChild(renderClientLinkModalItem(link, idx));
        });
      })
      .catch(err => {
        container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;">Error: ' + String(err) + '</div>';
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
      meta.textContent = link.transport + ' / Port ' + link.port;
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
      copyBtn.textContent = 'Copy';
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
      downloadBtn.textContent = 'Download';
      downloadBtn.addEventListener('click', () => downloadSingleClientLink(link));
      actions.appendChild(downloadBtn);
      const qrBtn = document.createElement('button');
      qrBtn.type = 'button';
      qrBtn.className = 'secondary';
      qrBtn.textContent = 'Show QR';
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
      document.getElementById('client-links-modal-overlay').classList.remove('active');
    };

    window.copyModalLink = function(inputId, btn) {
      const input = document.getElementById(inputId);
      input.select();
      input.setSelectionRange(0, 99999);
      try {
        navigator.clipboard.writeText(input.value).then(() => {
          const origText = btn.innerText;
          btn.innerText = 'Copied!';
          btn.style.background = 'var(--accent-success)';
          btn.style.color = '#fff';
          setTimeout(() => {
            btn.textContent = origText;
            btn.style.background = '';
            btn.style.color = '';
          }, 1500);
        });
      } catch (err) {
        alert('Failed to copy link: ' + String(err));
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
        container.textContent = 'Rendering QR...';
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
          image.alt = 'Client link QR code';
          image.style.width = '100%';
          image.style.height = '100%';
          image.style.border = '0';
          container.appendChild(image);
        } catch (err) {
          container.textContent = 'QR render failed: ' + String(err);
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
