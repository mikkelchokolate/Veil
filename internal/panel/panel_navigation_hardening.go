package panel

func panelNavigationInlineHandlerSlots() []RenderSlot {
	return []RenderSlot{
		{Placeholder: ` onclick="switchTab('dashboard')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('inbounds')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('routing')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('warp')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('diagnostics')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('backups')"`, Render: func() string { return "" }},
		{Placeholder: ` onclick="switchTab('users')"`, Render: func() string { return "" }},
	}
}

func panelNavigationHardeningJS() string {
	return `
    const veilPanelTabIds = ['dashboard', 'inbounds', 'routing', 'warp', 'diagnostics', 'backups', 'users'];
    const veilBaseSwitchTab = window.switchTab;

    window.switchTab = function(tabId) {
      const requestedTab = String(tabId || '');
      const safeTab = veilPanelTabIds.includes(requestedTab) ? requestedTab : 'dashboard';
      return veilBaseSwitchTab(safeTab);
    };

    function normalizedPanelTabFromLocation() {
      const requestedTab = window.location.hash.substring(1);
      if (veilPanelTabIds.includes(requestedTab)) return requestedTab;
      const dashboardURL = window.location.pathname + window.location.search + '#dashboard';
      window.history.replaceState(window.history.state, '', dashboardURL);
      return 'dashboard';
    }

    function syncPanelTabFromLocation() {
      return window.switchTab(normalizedPanelTabFromLocation());
    }

    document.querySelectorAll('.nav-menu .nav-item[href^="#"]').forEach((link) => {
      link.addEventListener('click', (event) => {
        const requestedTab = String(link.getAttribute('href') || '').replace(/^#/, '');
        if (!veilPanelTabIds.includes(requestedTab)) {
          event.preventDefault();
          window.switchTab('dashboard');
          return;
        }
        // A browser does not dispatch hashchange when the selected hash is
        // already active. Preserve the old sidebar behavior by refreshing the
        // tab loader explicitly on a repeated click.
        if (window.location.hash === '#' + requestedTab) {
          event.preventDefault();
          window.switchTab(requestedTab);
        }
      });
    });

    window.addEventListener('hashchange', syncPanelTabFromLocation);
    normalizedPanelTabFromLocation();
`
}
