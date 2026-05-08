package api

import "strings"

type StackSelectionCatalog struct {
	stacks []string
}

func NewStackSelectionCatalog() StackSelectionCatalog {
	return StackSelectionCatalog{stacks: []string{"panel", "mieru", "both", "naive", "hysteria2"}}
}

func (c StackSelectionCatalog) Stacks() []string {
	return append([]string(nil), c.stacks...)
}

func (c StackSelectionCatalog) Supports(stack string) bool {
	for _, allowed := range c.stacks {
		if allowed == stack {
			return true
		}
	}
	return false
}

func panelSettingsStackOptionsHTML() string {
	var b strings.Builder
	for _, stack := range NewStackSelectionCatalog().Stacks() {
		b.WriteString(`<option value="`)
		b.WriteString(stack)
		b.WriteString(`">`)
		b.WriteString(stack)
		b.WriteString("</option>\n")
	}
	return b.String()
}
