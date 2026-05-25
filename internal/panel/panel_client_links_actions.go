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
    window.openClientLinksModalFor = async function(inboundName) {
      document.getElementById('client-links-modal-title').innerText = 'Client Links: ' + inboundName;
      const container = document.getElementById('modal-links-container');
      container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;">Loading connection links...</div>';
      document.getElementById('client-links-modal-overlay').classList.add('active');
      
      const payload = { 
        inbound_name: inboundName 
      };

      fetch('/api/client-links', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...requestHeaders() },
        body: JSON.stringify(payload)
      })
      .then(res => res.json())
      .then(body => {
        if (!body.success) {
          container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;">' + (body.message || 'Failed to load links') + '</div>';
          return;
        }

        const links = body.links || [];
        if (links.length === 0) {
          container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 24px;">No connection links available for this inbound. Make sure the inbound is enabled and global settings are saved.</div>';
          return;
        }

        container.innerHTML = '';
        links.forEach((link, idx) => {
          const item = document.createElement('div');
          item.style.padding = '12px';
          item.style.marginBottom = '12px';
          item.style.border = '1px solid var(--border)';
          item.style.borderRadius = '8px';
          item.style.background = 'var(--surface)';

          const uniqueId = 'link-input-' + idx;
          const qrId = 'link-qr-' + idx;

          item.innerHTML = '<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">' +
              '<span style="font-weight: 600; color: #fff;">' + link.name + ' <span class="badge" style="margin-left: 8px; font-size: 0.75rem; padding: 3px 8px; background: rgba(79, 70, 229, 0.2); color: #818cf8;">' + link.protocol + '</span></span>' +
              '<span style="font-size: 0.8rem; color: var(--text-muted); text-transform: uppercase;">' + link.transport + ' &middot; Port ' + link.port + '</span>' +
            '</div>' +
            '<div style="display: flex; gap: 8px; margin-bottom: 12px;">' +
              '<input id="' + uniqueId + '" type="text" readOnly value="' + link.uri + '" style="flex: 1; font-family: monospace; font-size: 0.82rem; padding: 8px 12px; border-radius: 8px; background: var(--canvas); border: 1px solid var(--border); color: var(--text-main);">' +
              '<button type="button" class="dropdown-btn" onclick="copyModalLink(\'' + uniqueId + '\', this)" style="white-space: nowrap; font-size: 0.85rem; padding: 8px 14px; background: var(--primary); color: #000; border: 0; border-radius: 8px; cursor: pointer;">Copy</button>' +
            '</div>' +
            '<div style="display: flex; justify-content: flex-end;">' +
              '<button type="button" class="secondary" onclick="toggleQR(\'' + qrId + '\', \'' + encodeURIComponent(link.uri) + '\')" style="font-size: 0.78rem; padding: 6px 12px; border-radius: 6px; background: #27354f; color: var(--text-main); border: 0; cursor: pointer;">Show QR Code</button>' +
            '</div>' +
            '<div id="' + qrId + '" style="display: none; justify-content: center; margin-top: 16px; padding: 12px; background: #fff; border-radius: 8px; width: 150px; height: 150px; margin-left: auto; margin-right: auto; box-shadow: 0 4px 12px rgba(0,0,0,0.1);">' +
            '</div>';

          container.appendChild(item);
        });
      })
      .catch(err => {
        container.innerHTML = '<div style="color: var(--accent-danger); padding: 12px; text-align: center;">Error: ' + String(err) + '</div>';
      });
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

    window.toggleQR = function(qrId, encodedData) {
      const container = document.getElementById(qrId);
      if (container.style.display === 'none' || container.style.display === '') {
        container.innerHTML = '<img src="https://api.qrserver.com/v1/create-qr-code/?size=150x150&data=' + encodedData + '" alt="QR Code" style="width: 100%; height: 100%; border: 0;">';
        container.style.display = 'flex';
      } else {
        container.style.display = 'none';
      }
    };`
}
