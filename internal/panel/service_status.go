package panel

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/service"
)

const ServiceStatusCardPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_CARD__"
const ServiceStatusActionsPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_ACTIONS__"
const ServiceRestartActionsPlaceholder = "__VEIL_PANEL_SERVICE_RESTART_ACTIONS__"

func ServiceStatusCardHTML(runtimes []service.ManagedRuntime) string {
	return `    <div class="card">
      <h2>Service status</h2>
      <p>Read live systemd state for Veil, NaiveProxy/Caddy, Hysteria2, Mieru, and WARP/sing-box through <code>/api/status</code>.</p>
      <div class="actions">
        <button id="load-service-status" type="button">Load service status</button>
        <button id="toggle-auto-refresh" class="secondary" type="button">Auto-refresh: OFF</button>
      </div>
  <pre id="service-status-output" role="status" aria-live="polite">Not loaded</pre>
      <div class="actions">
` + ServiceRestartControlsHTML(runtimes) + `      </div>
    </div>`
}

func ServiceStatusActionsJS() string {
	return `    function loadServiceStatus() {
      return loadJSON('/api/status', 'service-status-output');
    }

    // Auto-refresh for service status (10s interval)
    let autoRefreshInterval = null;
    document.getElementById('toggle-auto-refresh').addEventListener('click', function() {
      const btn = this;
      if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
        btn.textContent = 'Auto-refresh: OFF';
        btn.classList.remove('danger');
        btn.classList.add('secondary');
      } else {
        loadServiceStatus(); // immediate refresh
        autoRefreshInterval = setInterval(loadServiceStatus, 10000);
        btn.textContent = 'Auto-refresh: ON (10s)';
        btn.classList.remove('secondary');
        btn.classList.add('danger');
      }
    });
    // Clean up interval on page unload
    window.addEventListener('beforeunload', function() {
      if (autoRefreshInterval) clearInterval(autoRefreshInterval);
    });`
}

func ServiceRestartControlsHTML(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	for _, runtime := range runtimes {
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

func ServiceRestartActionsJS(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	for _, runtime := range runtimes {
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
