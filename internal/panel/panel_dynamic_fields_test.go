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
		`type="number"`,
		`parseFloat(el.value)`,
		`isNaN(num)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic fields missing number handling %q", want)
		}
	}
}

func TestDynamicFieldsPreserveDataAttributesAndRequiredConstraints(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`normalized.indexOf('data-') === 0 ? normalized : 'data-' + normalized`,
		`const required = field.required ? ' required' : ''`,
		`opt.dataset.autoroom === 'true'`,
		`replace(/&/g, '&amp;')`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic fields missing schema attribute handling %q", want)
		}
	}
	if strings.Contains(js, `' data-' + k`) {
		t.Fatal("dynamic fields must not double-prefix existing data-* attributes")
	}
}
