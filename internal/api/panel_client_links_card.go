package api

const panelClientLinksCardPlaceholder = "__VEIL_PANEL_CLIENT_LINKS_CARD__"

func panelClientLinksCardHTML() string {
	return `    <div class="card">
      <h2>Client links</h2>
      <p>Generate current ` + NewInboundProtocolCatalog().DisplayNameList() + ` client connection URIs/client config artifacts from saved settings and enabled inbounds through <code>/api/client-links</code>.</p>
      <button id="load-client-links" type="button">Load client links</button>
      <button id="load-client-subscription" type="button">Load base64 subscription</button>
      <button id="load-client-subscription-raw" type="button">Load raw subscription</button>
      <button id="download-client-subscription" class="secondary" type="button">Download base64 subscription</button>
      <button id="download-client-subscription-raw" class="secondary" type="button">Download raw subscription</button>
      <button id="download-mieru-configs" class="secondary" type="button">Download Mieru client configs</button>
      <button id="copy-client-links" class="secondary" type="button">Copy output</button>
      <pre id="client-links-output">Not loaded</pre>
    </div>`
}
