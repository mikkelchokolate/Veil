package panel

import (
	"strings"
	"testing"
)

func TestLoginHTMLUsesSharedLocalizationRuntime(t *testing.T) {
	html := LoginHTML("/panel/", "ru")
	for _, want := range []string{
		`<html lang="ru">`,
		`data-veil-locale-select`,
		`window.veilLocale = "ru"`,
		`window.veilT`,
		`"/panel/api/auth/login"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login HTML missing %q", want)
		}
	}
}

func TestLoginHTMLSerializesSubmissionsUntilFailureOrReload(t *testing.T) {
	html := LoginHTML("/", "en")
	for _, want := range []string{
		`id="login-submit"`,
		`let loginInFlight = false;`,
		`if (loginInFlight) return;`,
		`loginInFlight = true;`,
		`let authenticated = false;`,
		`submitButton.disabled = true;`,
		`authenticated = true;`,
		`if (!authenticated) {`,
		`loginInFlight = false;`,
		`submitButton.disabled = false;`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login single-flight behavior missing %q", want)
		}
	}
}

func TestLoginHTMLPreservesTextAndStructuredAPIErrors(t *testing.T) {
	html := LoginHTML("/", "en")
	for _, want := range []string{
		`const text = await response.text();`,
		`data = JSON.parse(text);`,
		`data.message || (data.error && data.error.message) || text || ('HTTP ' + response.status)`,
		`if (!data.csrfToken || !data.username)`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login response handling missing %q", want)
		}
	}
	if strings.Contains(html, `const data = await resp.json();`) {
		t.Fatal("login must not lose text API errors through unconditional JSON parsing")
	}
}
