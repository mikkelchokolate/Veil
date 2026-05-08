package api

import "strings"

func panelServiceRestartControlsHTML() string {
	var b strings.Builder
	for _, runtime := range NewManagedRuntimeCatalog().Runtimes() {
		if !runtime.ManualRestart {
			continue
		}
		b.WriteString(`        <button id="restart-`)
		b.WriteString(runtime.ActionName)
		b.WriteString(`" class="danger" type="button">Restart `)
		b.WriteString(runtime.ActionName)
		b.WriteString("</button>\n")
	}
	return b.String()
}

func panelServiceRestartControlActionsJS() string {
	var b strings.Builder
	for _, runtime := range NewManagedRuntimeCatalog().Runtimes() {
		if !runtime.ManualRestart {
			continue
		}
		b.WriteString(`    document.getElementById('restart-`)
		b.WriteString(runtime.ActionName)
		b.WriteString(`').addEventListener('click', async () => {
      await loadJSON('/api/services/`)
		b.WriteString(runtime.ActionName)
		b.WriteString(`/restart', 'service-status-output', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });`)
		if runtime.ActionName == "veil" {
			b.WriteString(`
      loadServiceStatus();`)
		}
		b.WriteString(`
    });
`)
	}
	return b.String()
}
