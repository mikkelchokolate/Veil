package panel

func panelClientLinksReliabilityJS() string {
	return `
    let clientLinksModalRequestSequence = 0;
    let clientLinksModalController = null;
    const clientLinkQRControllers = new Map();

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
`
}
