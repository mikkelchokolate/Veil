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
