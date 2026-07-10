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
          navigation.style.display = viewer ? 'none' : '';
          navigation.setAttribute('aria-hidden', viewer ? 'true' : 'false');
        }
        if (section) {
          if (viewer && section.classList.contains('active')) {
            activeAdminTab = true;
          }
          section.style.display = viewer ? 'none' : '';
          section.setAttribute('aria-hidden', viewer ? 'true' : 'false');
        }
      });
      if (viewer && activeAdminTab) {
        switchTab('dashboard');
      }
    };
    applyViewerRoleGuard();
`
}
