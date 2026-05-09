package panel

import "github.com/veil-panel/veil/internal/protocols"

import "strings"

func panelInboundProtocolOptionsHTML() string {
	var b strings.Builder
	for _, choice := range protocols.NewCatalog().Choices() {
		b.WriteString(`                <option value="`)
		b.WriteString(choice.Protocol)
		b.WriteString(`">`)
		b.WriteString(choice.Protocol)
		b.WriteString("</option>\n")
	}
	return b.String()
}
