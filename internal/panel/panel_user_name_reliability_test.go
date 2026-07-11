package panel

import (
	"strings"
	"testing"
)

func TestPanelUserCredentialValidationMatchesServerContract(t *testing.T) {
	js := panelUserNameReliabilityJS()
	for _, want := range []string{
		`const byteLength = new TextEncoder().encode(value).length;`,
		`byteLength >= 3`,
		`byteLength <= 64`,
		`/^[\p{L}\p{N}._-]+$/u.test(value);`,
		`userNameInput.minLength = 3;`,
		`userNameInput.maxLength = 64;`,
		`function validPanelUserPassword(password) {`,
		`length >= 12;`,
		`userPasswordInput.minLength = 12;`,
		`const isEdit = document.getElementById('user-is-edit').value === 'true';`,
		`if ((!isEdit || password) && !validPanelUserPassword(password)) {`,
		`passwordInput.setCustomValidity(message);`,
		`const baseSaveUserWithNameValidation = saveUser;`,
		`return baseSaveUserWithNameValidation(event);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("user credential validation missing %q", want)
		}
	}
}

func TestRenderedPanelMountsUserCredentialValidationOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `function validPanelUsername(username)`); got != 1 {
		t.Fatalf("user name validator count = %d, want 1", got)
	}
	if got := strings.Count(html, `function validPanelUserPassword(password)`); got != 1 {
		t.Fatalf("user password validator count = %d, want 1", got)
	}
	if strings.Count(html, `const baseSaveUserWithNameValidation = saveUser;`) != 1 {
		t.Fatal("rendered Panel does not wrap user submission exactly once")
	}
}
