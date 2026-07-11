package panel

import "html/template"

// EscapeJavaScriptString escapes an untrusted value before it is inserted into
// an already-quoted inline JavaScript string literal.
func EscapeJavaScriptString(value string) string {
	return template.JSEscapeString(value)
}
