package panel

import (
	"strings"
	"testing"
)

func TestPanelRoleVisibilityHidesAdminOnlySectionsFromViewers(t *testing.T) {
	js := panelRoleVisibilityJS()
	for _, want := range []string{
		`const veilBaseApplyViewerRoleGuard = applyViewerRoleGuard`,
		`['backups', 'users']`,
		`link.hidden = viewer`,
		`section.hidden = viewer`,
		`window.location.hash === '#backups'`,
		`window.location.hash === '#users'`,
		`switchTab('dashboard')`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("role visibility JS missing %q", want)
		}
	}
	for _, visible := range []string{"diagnostics", "settings", "inbounds"} {
		if strings.Contains(js, `'#`+visible+`'`) {
			t.Fatalf("read-only-capable section %q must remain available to viewers", visible)
		}
	}
}

func TestPanelClientProfileActionsMountRoleVisibilityGuard(t *testing.T) {
	if !strings.Contains(panelClientProfileActionsJS(), `applyViewerRoleGuard = function()`) {
		t.Fatal("Panel runtime must mount the viewer section visibility guard")
	}
}
