package panel

func panelInboundControlsJS() string {
	return `
    const inboundModalOverlay = document.getElementById('inbound-modal-overlay');
    document.getElementById('add-inbound-btn').addEventListener('click', openAddInboundModal);
    document.getElementById('toggle-inbounds-raw').addEventListener('click', () => toggleRawView('inbounds-output'));
    document.getElementById('close-inbound-modal').addEventListener('click', closeInboundModal);
    document.getElementById('cancel-inbound-modal').addEventListener('click', closeInboundModal);
    document.getElementById('generate-inbound-password').addEventListener('click', genInboundPassword);
    if (inboundModalOverlay) {
      inboundModalOverlay.addEventListener('click', (event) => {
        if (event.target === inboundModalOverlay) closeInboundModal();
      });
    }
`
}
