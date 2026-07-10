package panel

import (
	"strings"
	"testing"
)

func TestViewerRoleGuardHidesAdminOnlyTabs(t *testing.T) {
	js := panelRoleTabVisibilityJS()
	for _, want := range []string{
		`const adminOnlyTabIds = ['backups', 'users'];`,
		`navigation.style.display = viewer ? 'none' : '';`,
		`section.style.display = viewer ? 'none' : '';`,
		`if (viewer && activeAdminTab)`,
		`switchTab('dashboard');`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("viewer tab guard missing %q", want)
		}
	}
}

func TestPanelCatalogMountsViewerTabVisibilityGuard(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `const adminOnlyTabIds = ['backups', 'users'];`) {
		t.Fatal("rendered Panel does not mount the viewer tab visibility guard")
	}
}
