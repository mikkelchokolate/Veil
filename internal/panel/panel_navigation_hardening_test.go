package panel

import (
	"strings"
	"testing"
)

func TestPanelNavigationRemovesInlineSidebarHandlers(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, tab := range []string{"dashboard", "inbounds", "routing", "warp", "diagnostics", "backups", "users"} {
		if !strings.Contains(html, `href="#`+tab+`"`) {
			t.Fatalf("rendered Panel missing sidebar link for %q", tab)
		}
		if strings.Contains(html, `onclick="switchTab('`+tab+`')"`) {
			t.Fatalf("rendered Panel still contains inline sidebar handler for %q", tab)
		}
	}
}

func TestPanelNavigationSynchronizesHashAndFallsBackSafely(t *testing.T) {
	js := panelNavigationHardeningJS()
	for _, want := range []string{
		`const veilPanelTabIds = ['dashboard', 'inbounds', 'routing', 'warp', 'diagnostics', 'backups', 'users'];`,
		`const veilBaseSwitchTab = window.switchTab;`,
		`window.switchTab = function(tabId)`,
		`const safeTab = veilPanelTabIds.includes(requestedTab) ? requestedTab : 'dashboard';`,
		`window.history.replaceState(window.history.state, '', dashboardURL);`,
		`window.addEventListener('hashchange', syncPanelTabFromLocation);`,
		`return window.switchTab(normalizedPanelTabFromLocation());`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("navigation hardening missing %q", want)
		}
	}
}

func TestPanelNavigationRefreshesRepeatedActiveTabClicks(t *testing.T) {
	js := panelNavigationHardeningJS()
	for _, want := range []string{
		`document.querySelectorAll('.nav-menu .nav-item[href^="#"]').forEach((link) => {`,
		`if (window.location.hash === '#' + requestedTab) {`,
		`event.preventDefault();`,
		`window.switchTab(requestedTab);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("navigation repeated-click refresh missing %q", want)
		}
	}
}

func TestSliceCatalogMountsNavigationHardening(t *testing.T) {
	catalog := NewSliceCatalog(nil)
	if _, ok := catalog.Slice("navigation"); !ok {
		t.Fatal("SliceCatalog missing navigation slice")
	}
	html := NewRenderer(catalog.RenderSlots()).BaseHTML()
	if !strings.Contains(html, `window.addEventListener('hashchange', syncPanelTabFromLocation);`) {
		t.Fatal("rendered Panel does not mount hash navigation hardening")
	}
}
