package panel

import (
	"strings"
	"testing"
)

func TestViewerRoleGuardHidesAdminOnlyTabs(t *testing.T) {
	js := panelRoleTabVisibilityJS()
	for _, want := range []string{
		`const adminOnlyTabIds = ['backups', 'users'];`,
		`navigation.hidden = viewer;`,
		`navigation.tabIndex = viewer ? -1 : 0;`,
		`section.hidden = viewer;`,
		`activeAdminTab || hashTargetsAdminTab`,
		`switchTab('dashboard');`,
		`window.history.replaceState(null, '', '#dashboard');`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("viewer tab guard missing %q", want)
		}
	}
	// The guard must actually run as the viewer (#838): it wraps and
	// re-invokes the base applyViewerRoleGuard, derives `viewer` from
	// isViewerRole(), and iterates the adminOnlyTabIds list — dropping any of
	// that wiring leaves admin tabs visible to viewers while the substring
	// checks above stay green.
	for _, want := range []string{
		`const baseApplyViewerRoleGuard = applyViewerRoleGuard;`,
		`const viewer = isViewerRole();`,
		`adminOnlyTabIds.forEach`,
		`applyViewerRoleGuard();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("viewer tab guard not wired to run as viewer, missing %q", want)
		}
	}
}

func TestPanelCatalogMountsViewerTabVisibilityGuardOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	marker := `const adminOnlyTabIds = ['backups', 'users'];`
	if count := strings.Count(html, marker); count != 1 {
		t.Fatalf("rendered Panel must mount exactly one viewer tab visibility guard, got %d", count)
	}
	if count := strings.Count(html, `applyViewerRoleGuard = function()`); count != 1 {
		t.Fatalf("rendered Panel must wrap applyViewerRoleGuard exactly once, got %d", count)
	}
}
