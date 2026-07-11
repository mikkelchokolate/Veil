package panel

import (
	"strings"
	"testing"
)

func TestReliableSetupHTMLMatchesCredentialContract(t *testing.T) {
	html := ReliableSetupHTML("/panel/", "en")
	for _, want := range []string{
		`"/panel/api/setup/complete"`,
		`id="setup-password" name="password" type="password" minlength="12" maxlength="72"`,
		`function validSetupUsername(username)`,
		`Array.from(value).length >= 3`,
		`byteLength <= 64`,
		`function validSetupPassword(password)`,
		`Array.from(value).length >= 12`,
		`new TextEncoder().encode(value).length <= 72`,
		`setupUsernameInput.setCustomValidity(message);`,
		`setupPasswordInput.setCustomValidity(message);`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("reliable setup HTML missing %q", want)
		}
	}
	if strings.Count(html, `function validSetupPassword(password)`) != 1 {
		t.Fatal("setup password validator must be mounted exactly once")
	}
}
