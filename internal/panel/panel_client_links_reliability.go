package panel

func panelClientLinksReliabilityJS() string {
	return `
    let clientLinksModalRequestSequence = 0;
    let clientLinksModalController = null;
    const clientLinkQRControllers = new Map();
    let clientLinksActionInFlight = false;
    let clientLinksOutputGeneration = 0;

    function setClientLinksModalMessage(container, message, isError) {
      container.textContent = '';
      const notice = document.createElement('div');
      notice.style.textAlign = 'center';
      notice.style.padding = '24px';
      notice.style.color = isError ? 'var(--accent-danger)' : 'var(--text-muted)';
      notice.textContent = message;
      container.appendChild(notice);
    }

    function filteredClientLinks(body, inboundName, inboundProtocol) {
      let links = body && Array.isArray(body.links) ? body.links : [];
      if (!inboundName) return links;
      return links.filter((link) => {
        const name = String(link && link.name || '');
        if (name === inboundName || name.indexOf(inboundName + '/') === 0) return true;
        return Boolean(inboundProtocol && name.indexOf(inboundProtocol + '/') === 0);
      });
    }

    function cancelClientLinksModalRequest() {
      clientLinksModalRequestSequence += 1;
      if (clientLinksModalController) {
        clientLinksModalController.abort();
        clientLinksModalController = null;
      }
    }

    function clearClientLinksModalQRs() {
      document.querySelectorAll('#modal-links-container [data-object-url]').forEach((container) => {
        URL.revokeObjectURL(container.dataset.objectUrl);
        delete container.dataset.objectUrl;
      });
      clientLinkQRControllers.forEach((controller) => controller.abort());
      clientLinkQRControllers.clear();
    }

    window.openClientLinksModalFor = async function(inboundName, inboundProtocol) {
      cancelClientLinksModalRequest();
      clearClientLinksModalQRs();
      const sequence = clientLinksModalRequestSequence;
      const controller = new AbortController();
      clientLinksModalController = controller;

      const title = document.getElementById('client-links-modal-title');
      const container = document.getElementById('modal-links-container');
      title.textContent = inboundName
        ? veilT('clientLinks.inboundTitle', { name: inboundName })
        : veilT('clientLinks.connectionTitle');
      setClientLinksModalMessage(container, veilT('clientLinks.loading'), false);
      openVeilDialog(document.getElementById('client-links-modal-overlay'));

      try {
        const response = await fetch('/api/client-links', {
          headers: requestHeaders(),
          signal: controller.signal
        });
        const text = await response.text();
        if (sequence !== clientLinksModalRequestSequence || controller.signal.aborted) return;
        if (!response.ok) {
          setClientLinksModalMessage(container, formatAPIError(text, response.status), true);
          return;
        }
        let body;
        try {
          body = text ? JSON.parse(text) : {};
        } catch (error) {
          setClientLinksModalMessage(container, veilT('common.error', { error: String(error) }), true);
          return;
        }
        if (body.message || body.error) {
          const message = body.message || (body.error && body.error.message) || veilT('clientLinks.failed');
          setClientLinksModalMessage(container, message, true);
          return;
        }
        const links = filteredClientLinks(body, inboundName, inboundProtocol);
        container.textContent = '';
        if (links.length === 0) {
          setClientLinksModalMessage(container, veilT('clientLinks.empty'), false);
          return;
        }
        links.forEach((link, index) => container.appendChild(renderClientLinkModalItem(link, index)));
      } catch (error) {
        if (error && error.name === 'AbortError') return;
        if (sequence !== clientLinksModalRequestSequence) return;
        setClientLinksModalMessage(container, veilT('common.error', { error: String(error) }), true);
      } finally {
        if (sequence === clientLinksModalRequestSequence) {
          clientLinksModalController = null;
        }
      }
    };

    window.closeClientLinksModal = function() {
      cancelClientLinksModalRequest();
      clearClientLinksModalQRs();
      closeVeilDialog(document.getElementById('client-links-modal-overlay'));
    };

    window.copyModalLink = async function(inputId, button) {
      const input = document.getElementById(inputId);
      if (!input) return;
      input.select();
      input.setSelectionRange(0, input.value.length);
      const originalText = button.textContent;
      try {
        await navigator.clipboard.writeText(input.value);
        button.textContent = veilT('clientLinks.copied');
        button.style.background = 'var(--accent-success)';
        button.style.color = '#fff';
        setTimeout(() => {
          button.textContent = originalText;
          button.style.background = '';
          button.style.color = '';
        }, 1500);
      } catch (error) {
        alert(veilT('clientLinks.copyFailedAlert', { error: String(error) }));
      }
    };

    window.toggleQR = async function(qrId, uri) {
      const container = document.getElementById(qrId);
      if (!container) return;
      const visible = container.style.display !== 'none' && container.style.display !== '';
      const existingController = clientLinkQRControllers.get(qrId);
      if (visible) {
        if (existingController) existingController.abort();
        clientLinkQRControllers.delete(qrId);
        if (container.dataset.objectUrl) {
          URL.revokeObjectURL(container.dataset.objectUrl);
          delete container.dataset.objectUrl;
        }
        container.style.display = 'none';
        container.textContent = '';
        return;
      }

      if (existingController) existingController.abort();
      const controller = new AbortController();
      clientLinkQRControllers.set(qrId, controller);
      container.textContent = veilT('clientLinks.renderingQR');
      container.style.display = 'flex';
      try {
        const response = await fetch('/api/client-links/qr', {
          method: 'POST',
          headers: requestHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify({ uri }),
          signal: controller.signal
        });
        if (!response.ok) {
          container.textContent = formatAPIError(await response.text(), response.status);
          return;
        }
        const blob = await response.blob();
        if (controller.signal.aborted || clientLinkQRControllers.get(qrId) !== controller || container.style.display === 'none') {
          return;
        }
        if (container.dataset.objectUrl) URL.revokeObjectURL(container.dataset.objectUrl);
        const url = URL.createObjectURL(blob);
        container.dataset.objectUrl = url;
        container.textContent = '';
        const image = document.createElement('img');
        image.src = url;
        image.alt = veilT('clientLinks.qrAlt');
        image.style.width = '100%';
        image.style.height = '100%';
        image.style.border = '0';
        container.appendChild(image);
      } catch (error) {
        if (error && error.name === 'AbortError') return;
        container.textContent = veilT('clientLinks.qrFailed', { error: String(error) });
      } finally {
        if (clientLinkQRControllers.get(qrId) === controller) {
          clientLinkQRControllers.delete(qrId);
        }
      }
    };

    function setClientLinksActionControlsDisabled(disabled) {
      [
        'load-client-links',
        'load-client-subscription',
        'load-client-subscription-raw',
        'download-client-links-json',
        'download-client-configs',
        'download-client-subscription',
        'download-client-subscription-raw'
      ].forEach((id) => {
        const button = document.getElementById(id);
        if (button) button.disabled = Boolean(disabled);
      });
    }

    function setClientLinksOutput(generation, value) {
      if (generation !== clientLinksOutputGeneration) return;
      const output = document.getElementById('client-links-output');
      if (output) output.textContent = String(value === undefined || value === null ? '' : value);
    }

    async function runClientLinksAction(action) {
      if (clientLinksActionInFlight) return null;
      clientLinksActionInFlight = true;
      const generation = ++clientLinksOutputGeneration;
      setClientLinksActionControlsDisabled(true);
      try {
        return await action(generation);
      } finally {
        clientLinksActionInFlight = false;
        setClientLinksActionControlsDisabled(false);
      }
    }

    async function fetchClientLinksText(path) {
      const response = await fetch(path, { headers: requestHeaders() });
      const text = await response.text();
      if (!response.ok) throw new Error(formatAPIError(text, response.status));
      return text;
    }

    function downloadClientLinksBlob(blob, filename) {
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = filename;
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    }

    loadClientLinks = async function() {
      return runClientLinksAction(async (generation) => {
        setClientLinksOutput(generation, veilT('status.loadingPath', { path: '/api/client-links' }));
        try {
          const text = await fetchClientLinksText('/api/client-links');
          const body = text ? JSON.parse(text) : {};
          setClientLinksOutput(generation, JSON.stringify(body, null, 2));
          return body;
        } catch (error) {
          setClientLinksOutput(generation, String(error && error.message ? error.message : error));
          return null;
        }
      });
    };

    loadClientSubscriptionPath = async function(path) {
      return runClientLinksAction(async (generation) => {
        setClientLinksOutput(generation, veilT('status.loadingPath', { path }));
        try {
          const text = await fetchClientLinksText(path);
          setClientLinksOutput(generation, text);
          return text;
        } catch (error) {
          setClientLinksOutput(generation, String(error && error.message ? error.message : error));
          return null;
        }
      });
    };

    downloadClientLinksJSON = async function() {
      return runClientLinksAction(async (generation) => {
        setClientLinksOutput(generation, veilT('clientLinks.downloadingJSON'));
        try {
          const text = await fetchClientLinksText('/api/client-links');
          const body = text ? JSON.parse(text) : {};
          downloadClientLinksBlob(
            new Blob([JSON.stringify(body, null, 2) + '\n'], { type: 'application/json;charset=utf-8' }),
            'veil-client-links.json'
          );
          setClientLinksOutput(generation, veilT('clientLinks.downloaded', { filename: 'veil-client-links.json' }));
          return true;
        } catch (error) {
          setClientLinksOutput(generation, veilT('status.downloadFailed', { error: String(error && error.message ? error.message : error) }));
          return null;
        }
      });
    };

    downloadClientConfigArtifacts = async function() {
      return runClientLinksAction(async (generation) => {
        setClientLinksOutput(generation, veilT('clientLinks.loadingClientConfigs'));
        try {
          const text = await fetchClientLinksText('/api/client-links');
          const body = text ? JSON.parse(text) : {};
          const artifacts = Array.isArray(body.artifacts) ? body.artifacts : [];
          const configs = [];
          artifacts.forEach((artifact) => {
            if (!artifact || artifact.kind !== 'client_config' || !artifact.content) return;
            let config;
            try {
              config = JSON.parse(artifact.content);
            } catch (error) {
              throw new Error('Invalid client config artifact ' + String(artifact.name || artifact.filename || 'unknown') + ': ' + String(error));
            }
            configs.push({ name: artifact.name, config });
          });
          if (configs.length === 0) {
            setClientLinksOutput(generation, veilT('clientLinks.noClientConfigs'));
            return null;
          }
          downloadClientLinksBlob(
            new Blob([JSON.stringify(configs, null, 2) + '\n'], { type: 'application/json;charset=utf-8' }),
            'veil-client-configs.json'
          );
          setClientLinksOutput(generation, veilT('clientLinks.downloaded', { filename: 'veil-client-configs.json' }));
          return true;
        } catch (error) {
          setClientLinksOutput(generation, veilT('clientLinks.clientConfigsFailed', { error: String(error && error.message ? error.message : error) }));
          return null;
        }
      });
    };

    downloadClientSubscriptionPath = async function(path, filename) {
      return runClientLinksAction(async (generation) => {
        setClientLinksOutput(generation, veilT('clientLinks.downloading', { path }));
        try {
          const text = await fetchClientLinksText(path);
          downloadClientLinksBlob(new Blob([text], { type: 'text/plain;charset=utf-8' }), filename);
          setClientLinksOutput(generation, veilT('clientLinks.downloaded', { filename }));
          return true;
        } catch (error) {
          setClientLinksOutput(generation, veilT('status.downloadFailed', { error: String(error && error.message ? error.message : error) }));
          return null;
        }
      });
    };
`
}
