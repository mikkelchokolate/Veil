package api

import "github.com/veil-panel/veil/internal/panel"

func renderPanelHTMLBase() string {
	return panel.NewRenderer(panel.NewSliceCatalog(NewManagedRuntimeCatalog().Runtimes()).RenderSlots()).BaseHTML()
}
