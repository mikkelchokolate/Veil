package panel

func panelRuntimeStatsVisibilityJS() string {
	return `
    const baseRefreshSystemTelemetry = refreshSystemTelemetry;
    refreshSystemTelemetry = async function() {
      const dashboard = document.getElementById('dashboard');
      if (document.hidden || !dashboard || !dashboard.classList.contains('active')) return;
      return baseRefreshSystemTelemetry();
    };

    document.addEventListener('visibilitychange', () => {
      if (!document.hidden && telemetryRefreshInterval) refreshSystemTelemetry();
    });

    document.querySelectorAll('[data-raw-output]').forEach((button) => {
      button.addEventListener('click', () => toggleRawView(button.dataset.rawOutput));
    });
`
}
