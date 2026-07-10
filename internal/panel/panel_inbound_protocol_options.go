package panel

import "github.com/mikkelchokolate/Veil/internal/protocols"

import "strings"

func panelInboundProtocolOptionsHTML() string {
	var b strings.Builder
	for _, choice := range protocols.NewCatalog().Choices() {
		b.WriteString(`                <option value="`)
		b.WriteString(choice.Protocol)
		b.WriteString(`">`)
		b.WriteString(choice.DisplayName)
		b.WriteString("</option>\n")
	}
	return b.String()
}
