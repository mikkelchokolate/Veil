package panel

// panelRoleVisibilityJS remains as a compatibility hook for the client-profile
// action bundle. Admin-only section visibility is mounted once by
// panelRoleTabVisibilityJS so applyViewerRoleGuard is not wrapped repeatedly.
func panelRoleVisibilityJS() string {
	return ""
}
