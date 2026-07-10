package panel

func panelRoleTabVisibilityJS() string {
	return `
    const adminOnlyTabIds = ['backups', 'users'];
    const baseApplyViewerRoleGuard = applyViewerRoleGuard;
    applyViewerRoleGuard = function() {
      baseApplyViewerRoleGuard();
      const viewer = isViewerRole();
      let activeAdminTab = false;
      adminOnlyTabIds.forEach((tabID) => {
        const navigation = document.querySelector('.nav-item[href="#' + tabID + '"]');
        const section = document.getElementById(tabID);
        if (navigation) {
          navigation.hidden = viewer;
          navigation.setAttribute('aria-hidden', viewer ? 'true' : 'false');
          navigation.tabIndex = viewer ? -1 : 0;
        }
        if (section) {
          if (viewer && section.classList.contains('active')) activeAdminTab = true;
          section.hidden = viewer;
          section.setAttribute('aria-hidden', viewer ? 'true' : 'false');
        }
      });
      const hashTargetsAdminTab = window.location.hash === '#backups' || window.location.hash === '#users';
      if (viewer && (activeAdminTab || hashTargetsAdminTab)) {
        switchTab('dashboard');
        if (window.history && typeof window.history.replaceState === 'function') {
          window.history.replaceState(null, '', '#dashboard');
        }
      }
    };
    applyViewerRoleGuard();
`
}
