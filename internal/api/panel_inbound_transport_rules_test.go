package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPanelInboundTransportRulesRenderFromProtocolCatalog(t *testing.T) {
	js := panelInboundProtocolTransportRulesJS()
	for _, choice := range NewInboundProtocolCatalog().Choices() {
		encoded, err := json.Marshal(choice.Transports)
		if err != nil {
			t.Fatal(err)
		}
		want := `"` + choice.Protocol + `":` + string(encoded)
		if !strings.Contains(js, want) {
			t.Fatalf("transport rules missing %q in %s", want, js)
		}
	}
	for _, want := range []string{
		`function syncInboundTransportOptions()`,
		`inbound-protocol`,
		`inbound-transport`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("transport rules missing %q in %s", want, js)
		}
	}
}
