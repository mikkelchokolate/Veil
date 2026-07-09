package panel

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/service"
)

const ServiceStatusCardPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_CARD__"
const ServiceStatusActionsPlaceholder = "__VEIL_PANEL_SERVICE_STATUS_ACTIONS__"
const ServiceRestartActionsPlaceholder = "__VEIL_PANEL_SERVICE_RESTART_ACTIONS__"

func ServiceStatusCardHTML(runtimes []service.ManagedRuntime) string {
	return "    \u003cdiv class=\"card\"\u003e\n" +
		"      \u003ch2\u003eService status\u003c/h2\u003e\n" +
		"      \u003cp\u003eRead live systemd state for managed services through \u003ccode\u003e/api/status\u003c/code\u003e.\u003c/p\u003e\n" +
		"      \u003cdiv class=\"actions\"\u003e\n" +
		"        \u003cbutton id=\"load-service-status\" type=\"button\"\u003eLoad service status\u003c/button\u003e\n" +
		"        \u003cbutton id=\"toggle-auto-refresh\" class=\"secondary\" type=\"button\"\u003eAuto-refresh: OFF\u003c/button\u003e\n" +
		"      \u003c/div\u003e\n" +
		"  \u003cpre id=\"service-status-output\" role=\"status\" aria-live=\"polite\"\u003eNot loaded\u003c/pre\u003e\n" +
		"      \u003cdiv class=\"actions\" id=\"service-restart-controls\"\u003e\n" +
		ServiceRestartControlsHTML(runtimes) + "      \u003c/div\u003e\n" +
		"    \u003c/div\u003e"
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
