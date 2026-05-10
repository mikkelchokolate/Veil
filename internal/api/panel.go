package api

import "github.com/veil-panel/veil/internal/panel"

// panelHTML returns the panel HTML with all API paths adjusted for the given base path.
// When basePath is "/", the HTML is returned unchanged.
func panelHTML(basePath string) string {
	return panel.NewRenderer(panel.NewSliceCatalog(NewManagedRuntimeCatalog().Runtimes()).RenderSlots()).HTML(basePath)
}
