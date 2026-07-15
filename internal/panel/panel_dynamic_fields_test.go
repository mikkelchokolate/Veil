package panel

import (
	"strings"
	"testing"
)

// TestDynamicFieldsHandlesNumberType verifies that the generic dynamic field
// renderer honors the schema.FieldNumber type: it renders a number input and
// parses the collected value back to a numeric type instead of leaving it as a
// string.
func TestDynamicFieldsHandlesNumberType(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`field.type === 'number'`,
		`input.type = 'number'`,
		`parseFloat(el.value)`,
		`isNaN(num)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic fields missing number handling %q", want)
		}
	}
}

func TestDynamicFieldsPreserveSafeDataAttributesAndRequiredConstraints(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`normalized.indexOf('data-') === 0 ? normalized : 'data-' + normalized`,
		`/^data-[a-z0-9_.:-]+$/.test(candidate)`,
		`option.setAttribute(attributeName, String(value))`,
		`input.required = Boolean(field.required)`,
		`opt.dataset.autoroom === 'true'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic fields missing safe schema attribute handling %q", want)
		}
	}
	if strings.Contains(js, `' data-' + k`) {
		t.Fatal("dynamic fields must not double-prefix existing data-* attributes")
	}
}

func TestDynamicFieldsDoNotRenderSchemaDataThroughInnerHTML(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`document.createElement('select')`,
		`document.createElement('option')`,
		`label.textContent = String(field.label || field.key)`,
		`btn.addEventListener('click'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic fields missing DOM rendering primitive %q", want)
		}
	}
	if strings.Contains(js, `innerHTML`) {
		t.Fatal("protocol schema data must not be rendered through innerHTML")
	}
}
