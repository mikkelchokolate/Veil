package api

import (
	"strings"
	"testing"
)

func TestPanelSettingsActionsDoNotWriteStack(t *testing.T) {
	actions := panelSettingsActionsJS()
	for _, unwanted := range []string{"stack:", "stack"} {
		if strings.Contains(actions, unwanted) {
			t.Fatalf("Settings actions should not write removed stack field %q:\n%s", unwanted, actions)
		}
	}
}

func TestPanelSettingsCardHidesStackSelection(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, unwanted := range []string{`id="settings-stack"`, `>Stack</label>`, `<option value="both">both</option>`, `<option value="mieru">mieru</option>`} {
		if strings.Contains(card, unwanted) {
			t.Fatalf("Settings card should not expose stack selection %q:\n%s", unwanted, card)
		}
	}
}
