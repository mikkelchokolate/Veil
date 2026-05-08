package api

import (
	"strings"
	"testing"
)

func TestPanelSettingsCardHidesStackSelection(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, unwanted := range []string{`id="settings-stack"`, `>Stack</label>`, `<option value="both">both</option>`, `<option value="mieru">mieru</option>`} {
		if strings.Contains(card, unwanted) {
			t.Fatalf("Settings card should not expose stack selection %q:\n%s", unwanted, card)
		}
	}
}
