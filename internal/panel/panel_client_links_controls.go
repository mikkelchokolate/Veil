package panel

import "strings"

func panelClientLinksHardenedCardHTML() string {
	html := panelClientLinksCardHTML()
	replacer := strings.NewReplacer(
		`<button type="button" class="modal-close" aria-label="Close dialog" onclick="closeClientLinksModal()">`,
		`<button id="close-client-links-modal" type="button" class="modal-close" aria-label="Close dialog">`,
		`<button class="secondary" type="button" onclick="closeClientLinksModal()">Close</button>`,
		`<button id="close-client-links-modal-footer" class="secondary" type="button">Close</button>`,
	)
	return replacer.Replace(html)
}

func panelClientLinksControlsJS() string {
	return `
    // Non-aggregate protocols encode the inbound name in every link name. Do
    // not fall back to all links from the same protocol, or a modal opened for
    // one inbound will expose links belonging to sibling inbounds. Mieru is the
    // exception: its client configs are intentionally aggregated by profile and
    // therefore use names such as "mieru/profile" instead of an inbound prefix.
    filteredClientLinks = function(body, inboundName, inboundProtocol) {
      const links = body && Array.isArray(body.links) ? body.links : [];
      if (!inboundName) return links;
      const exact = links.filter((link) => {
        const name = String(link && link.name || '');
        return name === inboundName || name.indexOf(inboundName + '/') === 0;
      });
      if (exact.length > 0 || inboundProtocol !== 'mieru') return exact;
      return links.filter((link) => {
        const name = String(link && link.name || '');
        return String(link && link.protocol || '') === 'mieru' && name.indexOf('mieru/') === 0;
      });
    };

    const clientLinksModalOverlay = document.getElementById('client-links-modal-overlay');
    const closeClientLinksModalButton = document.getElementById('close-client-links-modal');
    const closeClientLinksModalFooterButton = document.getElementById('close-client-links-modal-footer');

    if (closeClientLinksModalButton) {
      closeClientLinksModalButton.addEventListener('click', closeClientLinksModal);
    }
    if (closeClientLinksModalFooterButton) {
      closeClientLinksModalFooterButton.addEventListener('click', closeClientLinksModal);
    }
    if (clientLinksModalOverlay) {
      clientLinksModalOverlay.addEventListener('click', (event) => {
        if (event.target === clientLinksModalOverlay) closeClientLinksModal();
      });
    }
`
}
