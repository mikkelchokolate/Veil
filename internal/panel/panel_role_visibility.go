package panel

// panelRoleVisibilityJS extends the shared role guard for sections whose APIs
// are entirely admin-only. Hiding those tabs prevents viewer navigation from
// issuing predictable 403 requests while leaving read-only diagnostics visible.
func panelRoleVisibilityJS() string {
	return `    const veilBaseApplyViewerRoleGuard = applyViewerRoleGuard;
    applyViewerRoleGuard = function() {
      veilBaseApplyViewerRoleGuard();
      const viewer = isViewerRole();
      ['backups', 'users'].forEach((tabID) => {
        const link = document.querySelector('.nav-item[href="#' + tabID + '"]');
        const section = document.getElementById(tabID);
        if (link) {
          link.hidden = viewer;
          link.setAttribute('aria-hidden', viewer ? 'true' : 'false');
          link.tabIndex = viewer ? -1 : 0;
        }
        if (section) section.hidden = viewer;
      });
      if (viewer && (window.location.hash === '#backups' || window.location.hash === '#users')) {
        switchTab('dashboard');
        if (window.history && typeof window.history.replaceState === 'function') {
          window.history.replaceState(null, '', '#dashboard');
        }
      }
    };
    applyViewerRoleGuard();
`
}
