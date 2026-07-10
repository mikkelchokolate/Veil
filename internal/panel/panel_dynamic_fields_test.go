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
