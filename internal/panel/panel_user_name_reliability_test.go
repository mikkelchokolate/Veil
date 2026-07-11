package panel

import (
	"strings"
	"testing"
)

func TestPanelUserNameValidationMatchesServerContract(t *testing.T) {
	js := panelUserNameReliabilityJS()
	for _, want := range []string{
		`const byteLength = new TextEncoder().encode(value).length;`,
		`byteLength >= 3`,
		`byteLength <= 64`,
		`/^[\p{L}\p{N}._-]+$/u.test(value);`,
		`userNameInput.minLength = 3;`,
		`userNameInput.maxLength = 64;`,
		`const baseSaveUserWithNameValidation = saveUser;`,
		`input.setCustomValidity(message);`,
		`return baseSaveUserWithNameValidation(event);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("user name validation missing %q", want)
		}
	}
}

func TestRenderedPanelMountsUserNameValidationOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `function validPanelUsername(username)`); got != 1 {
		t.Fatalf("user name validator count = %d, want 1", got)
	}
	if strings.Count(html, `const baseSaveUserWithNameValidation = saveUser;`) != 1 {
		t.Fatal("rendered Panel does not wrap user submission exactly once")
	}
}
