package panel

import (
	"strings"
	"testing"
)

func TestRenderedPanelReloadsExpiredCookieSessionOnce(t *testing.T) {
	html := AuthenticationExpiryReliableHTML(StorageReliableHTML(NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()))
	for _, want := range []string{
		`const veilNativeFetch = window.fetch.bind(window);`,
		`if (veilStorage.getItem('veil_api_token')) return;`,
		`if (response && response.status === 401)`,
		`scheduleVeilAuthenticationReset();`,
		`veilStorage.removeItem('veil_csrf_token');`,
		`veilStorage.removeItem('veil_username');`,
		`veilStorage.removeItem('veil_user_role');`,
		`window.setTimeout(() => window.location.reload(), 100);`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel missing authentication-expiry marker %q", want)
		}
	}
	if got := strings.Count(html, `let veilAuthenticationReloadScheduled = false;`); got != 1 {
		t.Fatalf("authentication reset guard count = %d, want 1", got)
	}
	storageBridge := strings.Index(html, `const veilStorageFallback = new Map();`)
	authBridge := strings.Index(html, `const veilNativeFetch = window.fetch.bind(window);`)
	if storageBridge < 0 || authBridge < storageBridge {
		t.Fatalf("authentication bridge must follow storage bridge: storage=%d auth=%d", storageBridge, authBridge)
	}
}
