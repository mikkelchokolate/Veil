package api

import "strings"

func panelInboundProtocolOptionsHTML() string {
	var b strings.Builder
	for _, choice := range NewInboundProtocolCatalog().Choices() {
		b.WriteString(`                <option value="`)
		b.WriteString(choice.Protocol)
		b.WriteString(`">`)
		b.WriteString(choice.Protocol)
		b.WriteString("</option>\n")
	}
	return b.String()
}
