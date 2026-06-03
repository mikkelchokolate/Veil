package panel

import (
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"strings"
	"testing"
)

func TestPanelInboundProtocolOptionsRenderFromCatalog(t *testing.T) {
	html := panelInboundProtocolOptionsHTML()
	for _, choice := range protocols.NewCatalog().Choices() {
		want := `<option value="` + choice.Protocol + `">` + choice.Protocol + `</option>`
		if !strings.Contains(html, want) {
			t.Fatalf("protocol options missing %q in %s", want, html)
		}
	}
}
