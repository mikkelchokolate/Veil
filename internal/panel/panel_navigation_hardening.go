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

    window.addEventListener('hashchange', syncPanelTabFromLocation);
    normalizedPanelTabFromLocation();
`
}
