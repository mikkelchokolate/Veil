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
      <div class="actions" id="service-restart-controls">
` + ServiceRestartControlsHTML(runtimes) + `      </div>
    </div>`
}

func ServiceStatusActionsJS() string {
	return `    function loadServiceStatus() {
      return loadJSON('/api/status', 'service-status-output').then((status) => {
        if (status) {
          renderServiceRestartControls(status);
        }
        return status;
      });
    }

    // Auto-refresh for service status (10s interval)
    let autoRefreshInterval = null;
    document.getElementById('toggle-auto-refresh').addEventListener('click', function() {
      const btn = this;
      if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
        btn.textContent = veilT('service.autoRefreshOff');
        btn.classList.remove('danger');
        btn.classList.add('secondary');
      } else {
        loadServiceStatus(); // immediate refresh
        autoRefreshInterval = setInterval(loadServiceStatus, 10000);
        btn.textContent = veilT('service.autoRefreshOn');
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
		b.WriteString(`" class="danger" type="button" data-veil-restart-service="`)
		b.WriteString(runtime.ActionName)
		b.WriteString(`">`)
		b.WriteString(runtime.ActionName)
		b.WriteString("</button>\n")
	}
	return b.String()
}

func ServiceRestartActionsJS(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	b.WriteString(`    function escapeHTML(value) {
      const div = document.createElement('div');
      div.textContent = value == null ? '' : String(value);
      return div.innerHTML;
    }

    function bindServiceRestartButton(button) {
      if (!button || button.dataset.veilRestartBound === 'true') {
        return;
      }
      button.dataset.veilRestartBound = 'true';
      button.textContent = veilT('service.restart', { service: button.dataset.veilRestartService });
      button.addEventListener('click', async () => {
        const serviceName = button.dataset.veilRestartService;
        await loadJSON('/api/services/' + encodeURIComponent(serviceName) + '/restart', 'service-status-output', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true })
        });
        await loadServiceStatus();
      });
    }

    function renderServiceRestartControls(status) {
      const container = document.getElementById('service-restart-controls');
      if (!container || !status || !Array.isArray(status.services)) {
        return;
      }
      const restartable = status.services.filter((service) => {
        return service && service.restartable && (service.actionName || service.name);
      });
      container.innerHTML = restartable.map((service) => {
        const actionName = service.actionName || service.name;
        return '<button id="restart-' + escapeHTML(actionName) + '" class="danger" type="button" data-veil-restart-service="' + escapeHTML(actionName) + '">' + escapeHTML(actionName) + '</button>';
      }).join('\n');
      container.querySelectorAll('[data-veil-restart-service]').forEach(bindServiceRestartButton);
      if (typeof applyViewerRoleGuard === 'function') {
        applyViewerRoleGuard();
      }
    }

    document.querySelectorAll('[data-veil-restart-service]').forEach(bindServiceRestartButton);
`)
	return b.String()
}
