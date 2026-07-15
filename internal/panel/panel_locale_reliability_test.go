package panel

import (
	"strings"
	"testing"
)

func TestPanelLocalePersistenceWaitsForServerSuccess(t *testing.T) {
	js := panelLocalePersistenceReliabilityJS()
	for _, want := range []string{
		`document.addEventListener('change', async (event) => {`,
		`event.stopImmediatePropagation();`,
		`let panelLocaleChangeInFlight = false;`,
		`credentials: 'same-origin',`,
		`const text = await response.text();`,
		`throw new Error(formatAPIError(text, response.status));`,
		`select.value = window.veilLocale;`,
		`select.disabled = false;`,
		`alert(veilT('status.requestFailed', {`,
		`}, true);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("locale reliability missing %q", want)
		}
	}

	cookieWrite := strings.Index(js, `document.cookie = 'veil_locale='`)
	responseCheck := strings.Index(js, `if (!response.ok) {`)
	if cookieWrite < 0 || responseCheck < 0 || cookieWrite < responseCheck {
		t.Fatal("locale cookie is written before authenticated persistence succeeds")
	}
}

func TestRenderedPanelMountsLocaleReliabilityOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `let panelLocaleChangeInFlight = false;`); got != 1 {
		t.Fatalf("locale reliability mount count = %d, want 1", got)
	}
	if !strings.Contains(html, `event.stopImmediatePropagation();`) {
		t.Fatal("rendered Panel does not intercept the optimistic locale listener")
	}
}
