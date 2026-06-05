package panel

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPanelShellHasKeyboardAndMobileAccessibilityContract(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`class="skip-link" href="#main-content"`,
		`<nav class="nav-menu" aria-label="Primary navigation">`,
		`aria-current="page"`,
		`<button type="button" id="btn-logout"`,
		`<main id="main-content"`,
		`:focus-visible`,
		`@media (prefers-reduced-motion: reduce)`,
		`@media (max-width: 420px)`,
		`grid-template-columns: 1fr`,
		`width: min(600px, calc(100vw - 32px))`,
		`overflow-x: auto`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Panel accessibility contract missing %q", want)
		}
	}
}

func TestPanelDialogsHaveSemanticsAndAccessibleCloseButtons(t *testing.T) {
	dialogs := map[string]string{
		"inbound":      panelInboundFormHTML(),
		"client links": panelClientLinksCardHTML(),
		"routing":      panelRoutingCardHTML(),
	}
	for name, html := range dialogs {
		for _, want := range []string{
			`aria-hidden="true"`,
			`role="dialog"`,
			`aria-modal="true"`,
			`aria-labelledby="`,
			`tabindex="-1"`,
			`aria-label="Close dialog"`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("%s dialog missing %q", name, want)
			}
		}
	}
}

func TestSharedDialogManagerTrapsAndRestoresFocus(t *testing.T) {
	js := panelUtilityActionsJS()
	for _, want := range []string{
		`function openVeilDialog`,
		`function closeVeilDialog`,
		`veilPreviouslyFocused`,
		`event.key === 'Escape'`,
		`event.key !== 'Tab'`,
		`focusableElements`,
		`aria-hidden`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("dialog manager missing %q", want)
		}
	}

	for name, js := range map[string]string{
		"inbound":      panelInboundActionsJS(),
		"client links": panelClientLinksActionsJS(),
		"routing":      panelRoutingActionsJS(),
	} {
		if !strings.Contains(js, "openVeilDialog(") || !strings.Contains(js, "closeVeilDialog(") {
			t.Errorf("%s actions do not use shared dialog manager", name)
		}
	}
}

func TestAuthenticationPagesExposeFocusAndLiveStatus(t *testing.T) {
	for name, html := range map[string]string{
		"login": LoginHTML("/", LocaleEnglish),
		"setup": SetupHTML("/", LocaleEnglish),
	} {
		for _, want := range []string{`:focus-visible`, `@media (prefers-reduced-motion: reduce)`, `aria-live="polite"`} {
			if !strings.Contains(html, want) {
				t.Errorf("%s page missing %q", name, want)
			}
		}
	}
}

func TestAsyncPanelOutputsAreLiveStatusRegions(t *testing.T) {
	rendered := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	document, err := html.Parse(strings.NewReader(rendered))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"speedtest-output",
		"dns-lookup-output",
		"ping-output",
		"firewall-output",
		"logs-output",
		"apply-plan-output",
		"backup-output",
		"client-links-output",
		"inbounds-output",
		"routing-output",
		"token-rotation-output",
		"user-output",
		"warp-output",
		"service-status-output",
	} {
		node := findElementByID(document, id)
		if node == nil {
			t.Errorf("missing output %q", id)
			continue
		}
		if attributeValue(node, "role") != "status" || attributeValue(node, "aria-live") != "polite" {
			t.Errorf("%s is not a polite status region", id)
		}
	}
}

func findElementByID(node *html.Node, id string) *html.Node {
	if node.Type == html.ElementNode && attributeValue(node, "id") == id {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func attributeValue(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
