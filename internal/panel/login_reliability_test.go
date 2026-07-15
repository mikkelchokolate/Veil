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
	if authenticated < 0 {
		t.Fatal("login success marker is missing")
	}
	loginSuccess := html[authenticated:]
	storageGuard := strings.Index(loginSuccess, `try {
          localStorage.setItem('veil_csrf_token'`)
	reload := strings.Index(loginSuccess, `window.location.reload();`)
	if storageGuard < 0 || reload < storageGuard {
		t.Fatalf("login success ordering is unsafe: storage=%d reload=%d", storageGuard, reload)
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
