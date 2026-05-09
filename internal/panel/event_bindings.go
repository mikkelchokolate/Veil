package panel

import "strings"

type EventBinding struct {
	ElementID string
	Handler   string
	Event     string
}

func RenderEventBindings(bindings []EventBinding) string {
	var b strings.Builder
	for _, binding := range bindings {
		b.WriteString("    document.getElementById('")
		b.WriteString(binding.ElementID)
		b.WriteString("').addEventListener('")
		b.WriteString(binding.Event)
		b.WriteString("', ")
		b.WriteString(binding.Handler)
		b.WriteString(");\n")
	}
	return b.String()
}
