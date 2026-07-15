package panel

import (
	"strings"
	"testing"
)

func TestClientLinksInboundFilterDoesNotLeakSiblingProtocols(t *testing.T) {
	js := panelClientLinksControlsJS()
	for _, want := range []string{
		`filteredClientLinks = function(body, inboundName, inboundProtocol)`,
		`name === inboundName || name.indexOf(inboundName + '/') === 0`,
		`if (exact.length > 0 || inboundProtocol !== 'mieru') return exact;`,
		`String(link && link.protocol || '') === 'mieru'`,
		`name.indexOf('mieru/') === 0`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client-links inbound filter missing %q", want)
		}
	}
	if strings.Contains(js, `name.indexOf(inboundProtocol + '/') === 0`) {
		t.Fatal("client-links filter still falls back to every sibling link for ordinary protocols")
	}
}

func TestRenderedPanelMountsInboundClientLinkFilterOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `filteredClientLinks = function(body, inboundName, inboundProtocol)`); got != 1 {
		t.Fatalf("client-links inbound filter count = %d, want 1", got)
	}
}
