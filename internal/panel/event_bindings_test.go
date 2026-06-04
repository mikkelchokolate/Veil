package panel

import (
	"strings"
	"testing"
)

func TestRenderEventBindingsBuildsDOMEventWiring(t *testing.T) {
	js := RenderEventBindings([]EventBinding{{ElementID: "save-form", Event: "submit", Handler: "saveForm"}})
	if !strings.Contains(js, "const el = document.getElementById('save-form')") || !strings.Contains(js, "if (el) el.addEventListener('submit', saveForm);") {
		t.Fatalf("unexpected event binding JS:\n%s", js)
	}
}

func TestEventBindingsJSRendersCrossModuleBindings(t *testing.T) {
	js := EventBindingsJS(NewSliceCatalog(nil).EventBindings())
	for _, want := range []string{
		`document.querySelectorAll('[data-load]')`,
		`load-client-links`,
		`open-client-links-modal`,
		`download-client-links-json`,
		`inbound-form`,
		`routing-rule-form`,
		`warp-form`,
		`loadServiceStatus();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("event bindings missing %q:\n%s", want, js)
		}
	}
}
