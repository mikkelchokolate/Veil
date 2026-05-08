package api

import (
	"encoding/json"
	"strings"
)

type StackSelection struct {
	Name           string
	RequiresDomain bool
	Protocols      []string
	AllProtocols   bool
}

type StackSelectionCatalog struct {
	selections []StackSelection
}

func NewStackSelectionCatalog() StackSelectionCatalog {
	return StackSelectionCatalog{selections: []StackSelection{
		{Name: "panel"},
		{Name: "mieru", Protocols: []string{"mieru"}},
		{Name: "both", RequiresDomain: true, AllProtocols: true},
		{Name: "naive", RequiresDomain: true, Protocols: []string{"naiveproxy"}},
		{Name: "hysteria2", RequiresDomain: true, Protocols: []string{"hysteria2"}},
	}}
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
	selection, ok := c.selection(stack)
	if !ok {
		return false
	}
	if selection.AllProtocols {
		return NewInboundProtocolCatalog().Supports(protocol)
	}
	for _, allowed := range selection.Protocols {
		if allowed == protocol {
			return true
		}
	}
	return false
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

func panelProfilePreviewDomainRequirementsJS() string {
	requirements := map[string]bool{}
	for _, selection := range NewStackSelectionCatalog().Selections() {
		requirements[selection.Name] = selection.RequiresDomain
	}
	encoded, _ := json.Marshal(requirements)
	return `    const profilePreviewDomainRequired = ` + string(encoded) + `;

`
}
