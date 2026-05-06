package api

import (
	"strings"
	"testing"
)

func TestPanelHTMLIncludesInboundPasswordGenerationUI(t *testing.T) {
	html := panelHTML("/secret/")
	for _, want := range []string{
		`id="inbound-password"`,
		`genInboundPassword()`,
		`Generate`,
		`auto-generated if empty`,
		`payload.password`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel HTML missing %q", want)
		}
	}
}
