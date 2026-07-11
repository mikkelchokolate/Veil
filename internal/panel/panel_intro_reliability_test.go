package panel

import (
	"strings"
	"testing"
)

func TestIntroReliabilityTreatsRestartedSessionAsOnline(t *testing.T) {
	actions := panelIntroReliableActionsJS()
	for _, want := range []string{
		`cache: 'no-store'`,
		`checkResp.ok || checkResp.status === 401 || checkResp.status === 403`,
		`Authentication session reset after restart.`,
		`invalidateCurrentUserRoleRefresh();`,
		`localStorage.removeItem('veil_csrf_token');`,
		`localStorage.removeItem('veil_username');`,
		`localStorage.removeItem('veil_user_role');`,
		`setTimeout(() => window.location.reload(), 100);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("update restart reliability missing %q", want)
		}
	}
	if strings.Contains(actions, `localStorage.removeItem('veil_api_token');
                setTimeout(() => window.location.reload(), 100);`) {
		t.Fatal("update restart cleanup must preserve the configured static API token")
	}
}

func TestIntroReliabilityGuardsImmediateWarpToggle(t *testing.T) {
	actions := panelIntroReliableActionsJS()
	marker := `      'save-warp-config',
      'warp-enabled',
      'apply-staged-files',`
	if !strings.Contains(actions, marker) {
		t.Fatal("immediate WARP toggle is not included in the admin-only control guard")
	}
}

func TestRenderedPanelMountsIntroReliabilityOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `checkResp.ok || checkResp.status === 401 || checkResp.status === 403`); got != 1 {
		t.Fatalf("update restart reliability count = %d, want 1", got)
	}
	if got := strings.Count(html, `'warp-enabled',`); got != 1 {
		t.Fatalf("WARP admin guard count = %d, want 1", got)
	}
}
