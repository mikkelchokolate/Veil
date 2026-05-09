package panel

import (
	"strings"
	"testing"
)

func TestRenderEventBindingsBuildsDOMEventWiring(t *testing.T) {
	js := RenderEventBindings([]EventBinding{{ElementID: "save-form", Event: "submit", Handler: "saveForm"}})
	if !strings.Contains(js, "document.getElementById('save-form').addEventListener('submit', saveForm);") {
		t.Fatalf("unexpected event binding JS:\n%s", js)
	}
}
