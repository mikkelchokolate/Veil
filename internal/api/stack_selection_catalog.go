package api

import "strings"

type StackSelection struct {
	Name           string
	RequiresDomain bool
}

type StackSelectionCatalog struct {
	selections []StackSelection
}

func NewStackSelectionCatalog() StackSelectionCatalog {
	return StackSelectionCatalog{selections: []StackSelection{{Name: "panel"}}}
}

func (c StackSelectionCatalog) Selections() []StackSelection {
	return append([]StackSelection(nil), c.selections...)
}

func (c StackSelectionCatalog) Stacks() []string {
	stacks := make([]string, 0, len(c.selections))
	for _, selection := range c.selections {
		stacks = append(stacks, selection.Name)
	}
	return stacks
}

func (c StackSelectionCatalog) Supports(stack string) bool {
	_, ok := c.selection(stack)
	return ok
}

func (c StackSelectionCatalog) RequiresDomain(stack string) bool {
	selection, ok := c.selection(stack)
	return ok && selection.RequiresDomain
}

func (c StackSelectionCatalog) IncludesProtocol(stack string, protocol string) bool {
	return NewInboundProtocolCatalog().Supports(protocol)
}

func (c StackSelectionCatalog) selection(stack string) (StackSelection, bool) {
	for _, selection := range c.selections {
		if selection.Name == stack {
			return selection, true
		}
	}
	return StackSelection{}, false
}

func panelStackOptionsHTML() string {
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

func panelSettingsStackOptionsHTML() string {
	return panelStackOptionsHTML()
}
