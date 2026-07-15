package panel

import (
	"html"
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
	return `    let serviceStatusLoadInFlight = false;
    let serviceRestartInFlight = false;

    function setServiceStatusControlsDisabled(disabled) {
      const busy = Boolean(disabled);
      const loadButton = document.getElementById('load-service-status');
      if (loadButton) loadButton.disabled = busy;
      document.querySelectorAll('[data-veil-restart-service]').forEach((button) => {
        button.disabled = busy || isViewerRole();
      });
    }

    async function fetchAndRenderServiceStatus() {
      const status = await loadJSON('/api/status', 'service-status-output');
      if (status) renderServiceRestartControls(status);
      return status;
    }

    async function loadServiceStatus() {
      if (serviceStatusLoadInFlight || serviceRestartInFlight) return null;
      serviceStatusLoadInFlight = true;
      setServiceStatusControlsDisabled(true);
      try {
        return await fetchAndRenderServiceStatus();
      } finally {
        serviceStatusLoadInFlight = false;
        setServiceStatusControlsDisabled(serviceRestartInFlight);
        applyViewerRoleGuard();
      }
    }

    function refreshServiceStatusAutomatically() {
      const dashboard = document.getElementById('dashboard');
      if (document.hidden || !dashboard || !dashboard.classList.contains('active')) return null;
      return loadServiceStatus();
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
        refreshServiceStatusAutomatically();
        autoRefreshInterval = setInterval(refreshServiceStatusAutomatically, 10000);
        btn.textContent = veilT('service.autoRefreshOn');
        btn.classList.remove('secondary');
        btn.classList.add('danger');
      }
    });
    document.addEventListener('visibilitychange', function() {
      if (!document.hidden && autoRefreshInterval) refreshServiceStatusAutomatically();
    });
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
		actionName := html.EscapeString(runtime.ActionName)
		b.WriteString(`        <button id="restart-`)
		b.WriteString(actionName)
		b.WriteString(`" class="danger" type="button" data-veil-restart-service="`)
		b.WriteString(actionName)
		b.WriteString(`">`)
		b.WriteString(actionName)
		b.WriteString("</button>\n")
	}
	return b.String()
}

func ServiceRestartActionsJS(runtimes []service.ManagedRuntime) string {
	var b strings.Builder
	b.WriteString(`    function bindServiceRestartButton(button) {
      if (!button || button.dataset.veilRestartBound === 'true') return;
      button.dataset.veilRestartBound = 'true';
      button.textContent = veilT('service.restart', { service: button.dataset.veilRestartService });
      button.addEventListener('click', async () => {
        if (serviceRestartInFlight || serviceStatusLoadInFlight) return;
        const serviceName = button.dataset.veilRestartService;
        serviceRestartInFlight = true;
        setServiceStatusControlsDisabled(true);
        try {
          const restarted = await loadJSON('/api/services/' + encodeURIComponent(serviceName) + '/restart', 'service-status-output', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ confirm: true })
          });
          if (restarted) await fetchAndRenderServiceStatus();
        } finally {
          serviceRestartInFlight = false;
          setServiceStatusControlsDisabled(false);
          applyViewerRoleGuard();
        }
      });
    }

    function renderServiceRestartControls(status) {
      const container = document.getElementById('service-restart-controls');
      if (!container || !status || !Array.isArray(status.services)) return;
      const restartable = status.services.filter((service) => service && service.restartable && (service.actionName || service.name));
      container.textContent = '';
      restartable.forEach((service) => {
        const actionName = String(service.actionName || service.name);
        const button = document.createElement('button');
        button.id = 'restart-' + actionName;
        button.className = 'danger';
        button.type = 'button';
        button.dataset.veilRestartService = actionName;
        button.textContent = actionName;
        container.appendChild(button);
        bindServiceRestartButton(button);
      });
      setServiceStatusControlsDisabled(serviceStatusLoadInFlight || serviceRestartInFlight);
      if (typeof applyViewerRoleGuard === 'function') applyViewerRoleGuard();
    }

    document.querySelectorAll('[data-veil-restart-service]').forEach(bindServiceRestartButton);
`)
	return b.String()
}
