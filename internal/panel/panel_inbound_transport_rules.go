package panel

import "github.com/veil-panel/veil/internal/protocols"

import (
	"encoding/json"
	"strings"
)

func panelInboundProtocolTransportRulesJS() string {
	rules := map[string][]string{}
	for _, choice := range protocols.NewCatalog().Choices() {
		rules[choice.Protocol] = choice.Transports
	}
	encoded, _ := json.Marshal(rules)
	return `    const inboundProtocolTransports = ` + string(encoded) + `;

    function syncInboundTransportOptions() {
      const protocolSelect = document.getElementById('inbound-protocol');
      const transportSelect = document.getElementById('inbound-transport');
      const transports = inboundProtocolTransports[protocolSelect.value] || [];
      const previous = transportSelect.value;
      transportSelect.innerHTML = '';
      transports.forEach((transport) => {
        const option = document.createElement('option');
        option.value = transport;
        option.textContent = transport;
        transportSelect.appendChild(option);
      });
      if (transports.includes(previous)) {
        transportSelect.value = previous;
      }
    }
`
}

func panelInboundTransportOptionsHTML() string {
	seen := map[string]bool{}
	var b strings.Builder
	for _, choice := range protocols.NewCatalog().Choices() {
		for _, transport := range choice.Transports {
			if seen[transport] {
				continue
			}
			seen[transport] = true
			b.WriteString(`                <option value="`)
			b.WriteString(transport)
			b.WriteString(`">`)
			b.WriteString(transport)
			b.WriteString("</option>\n")
		}
	}
	return b.String()
}
