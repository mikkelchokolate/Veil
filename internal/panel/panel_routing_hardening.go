package panel

import "strings"

func panelRoutingHardenedCardHTML() string {
	return strings.NewReplacer(
		`id="add-routing-rule-btn" type="button" onclick="openRoutingModal(null)"`,
		`id="add-routing-rule-btn" type="button"`,
		`<button class="secondary" type="button" onclick="loadRoutingRules()">`,
		`<button id="load-routing-rules" class="secondary" type="button">`,
		`<button class="btn-console-clear" type="button" onclick="document.getElementById('routing-output').textContent = 'Console cleared.'">`,
		`<button id="clear-routing-output" class="btn-console-clear" type="button">`,
		`<div id="routing-modal" class="modal-overlay" aria-hidden="true" onclick="if(event.target === this) closeRoutingModal()">`,
		`<div id="routing-modal" class="modal-overlay" aria-hidden="true">`,
		`<button type="button" class="modal-close" aria-label="Close dialog" onclick="closeRoutingModal()">`,
		`<button id="close-routing-modal" type="button" class="modal-close" aria-label="Close dialog">`,
	).Replace(panelRoutingCardHTML())
}

func panelRoutingControlsJS() string {
	return `
    document.getElementById('add-routing-rule-btn').addEventListener('click', () => openRoutingModal(null));
    document.getElementById('load-routing-rules').addEventListener('click', loadRoutingRules);
    document.getElementById('clear-routing-output').addEventListener('click', () => {
      const output = document.getElementById('routing-output');
      if (output) output.textContent = 'Console cleared.';
    });
    document.getElementById('close-routing-modal').addEventListener('click', closeRoutingModal);
    document.getElementById('routing-modal').addEventListener('click', (event) => {
      if (event.target === event.currentTarget) closeRoutingModal();
    });
`
}
