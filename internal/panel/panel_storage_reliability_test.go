package panel

import (
	"strings"
	"testing"
)

func TestRenderedPanelUsesSafeStorageAdapter(t *testing.T) {
	html := StorageReliableHTML(NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML())
	for _, want := range []string{
		`const veilStorageFallback = new Map();`,
		`window.localStorage.getItem(key)`,
		`window.localStorage.setItem(key, text)`,
		`window.localStorage.removeItem(key)`,
		`veilStorage.getItem('veil_api_token')`,
		`veilStorage.setItem('veil_user_role', role)`,
		`veilStorage.removeItem('veil_csrf_token')`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel missing storage reliability marker %q", want)
		}
	}

	bridge := strings.Index(html, `const veilStorageFallback = new Map();`)
	firstUse := strings.Index(html, `veilStorage.getItem(`)
	if bridge < 0 || firstUse < 0 || bridge > firstUse {
		t.Fatalf("storage bridge must be initialized before use: bridge=%d firstUse=%d", bridge, firstUse)
	}

	for _, bridgeCall := range []string{
		`window.localStorage.getItem(`,
		`window.localStorage.setItem(`,
		`window.localStorage.removeItem(`,
	} {
		if got := strings.Count(html, bridgeCall); got != 1 {
			t.Fatalf("storage bridge call %q count = %d, want 1", bridgeCall, got)
		}
	}
}
