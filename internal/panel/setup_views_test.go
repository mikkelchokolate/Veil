package panel

import (
	"strings"
	"testing"
)

func TestSetupHTMLContainsAccessibleFirstRunControls(t *testing.T) {
	html := SetupHTML("/panel/", "ru")
	for _, want := range []string{
		`id="setup-form"`,
		`for="setup-username"`,
		`id="setup-username"`,
		`for="setup-password"`,
		`id="setup-password"`,
		`id="setup-backup-ack"`,
		`aria-live="polite"`,
		`"/panel/api/setup/complete"`,
		"Create administrator",
		"Local access",
		"Backup and recovery",
		`<html lang="ru">`,
		`data-veil-locale-select`,
		`window.veilLocale = "ru"`,
		`window.veilT`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("setup HTML missing %q", want)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Fatalf("setup HTML should not load external resources")
	}
}

func TestSetupHTMLKeepsSuccessfulSubmissionLockedUntilReload(t *testing.T) {
	html := SetupHTML("/", "en")
	for _, want := range []string{
		`let setupInFlight = false;`,
		`if (setupInFlight) return;`,
		`setupInFlight = true;`,
		`let completed = false;`,
		`completed = true;`,
		`if (!completed) {`,
		`setupInFlight = false;`,
		`button.disabled = false;`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("setup single-flight behavior missing %q", want)
		}
	}
}
