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
