package panel

import (
	"strings"
	"testing"
)

func TestSetupHTMLContainsAccessibleFirstRunControls(t *testing.T) {
	html := SetupHTML("/panel/")
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
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("setup HTML missing %q", want)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Fatalf("setup HTML should not load external resources")
	}
}
