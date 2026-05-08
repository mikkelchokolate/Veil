package api

import "strings"

func renderPanelHTMLBase() string {
	html := panelHTMLBase
	for _, slot := range NewPanelRenderSlotCatalog().Slots() {
		html = strings.ReplaceAll(html, slot.Placeholder, slot.Render())
	}
	return html
}
