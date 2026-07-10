package panel

// panelUserIdentityCleanupJS extends self-session/user cleanup to remove a
// locally configured static token as well. Otherwise a reload after revoking or
// editing the current account can silently authenticate again as token admin.
func panelUserIdentityCleanupJS() string {
	return `
    const baseClearStoredPanelIdentity = clearStoredPanelIdentity;
    clearStoredPanelIdentity = function() {
      baseClearStoredPanelIdentity();
      localStorage.removeItem('veil_api_token');
      const tokenField = document.getElementById('api-token');
      if (tokenField) tokenField.value = '';
    };
`
}
