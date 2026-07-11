package panel

import (
	"strings"
	"testing"
)

func TestReliableLoginHTMLTreatsStorageAsBestEffort(t *testing.T) {
	html := ReliableLoginHTML("/panel/", "en")
	for _, want := range []string{
		`fetch("/panel/api/auth/login"`,
		`authenticated = true;`,
		`catch (storageError)`,
		`Could not persist login metadata.`,
		`catch (cookieError)`,
		`Could not persist login locale.`,
		`window.location.reload();`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("reliable login HTML missing %q", want)
		}
	}

	authenticated := strings.Index(html, `authenticated = true;`)
	storageGuard := strings.Index(html, `try {
          localStorage.setItem('veil_csrf_token'`)
	reload := strings.Index(html, `window.location.reload();`)
	if authenticated < 0 || storageGuard < authenticated || reload < storageGuard {
		t.Fatalf("login success ordering is unsafe: authenticated=%d storage=%d reload=%d", authenticated, storageGuard, reload)
	}
}

func TestReliableLoginHTMLRemovesUnguardedStorageSequence(t *testing.T) {
	html := ReliableLoginHTML("/", "en")
	unguarded := `        authenticated = true;
        localStorage.setItem('veil_csrf_token', data.csrfToken);`
	if strings.Contains(html, unguarded) {
		t.Fatal("login still writes localStorage outside the best-effort guard")
	}
}
