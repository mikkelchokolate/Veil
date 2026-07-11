package panel

import "strings"

// panelIntroReliableActionsJS hardens controls that live in the shared intro
// runtime. WARP's immediate toggle is a mutation even though it is not a form
// submit, and an update restart invalidates in-memory cookie sessions and CSRF
// state even when the service has already returned successfully.
func panelIntroReliableActionsJS() string {
	actions := panelIntroActionsJS()
	actions = strings.Replace(actions, `      'save-warp-config',
      'apply-staged-files',`, `      'save-warp-config',
      'warp-enabled',
      'apply-staged-files',`, 1)
	actions = strings.Replace(actions, `        if (data && data.authenticated) {
          setCurrentUserRole(data.role || '');
        } else if (await staticTokenHasAdminAccess()) {`, `        if (data && data.authenticated) {
          const refreshedCSRFToken = String(data.csrfToken || '');
          window.veil_csrf_token = refreshedCSRFToken;
          if (refreshedCSRFToken) {
            localStorage.setItem('veil_csrf_token', refreshedCSRFToken);
          } else {
            localStorage.removeItem('veil_csrf_token');
          }
          const refreshedUsername = String(data.username || '');
          if (refreshedUsername) {
            localStorage.setItem('veil_username', refreshedUsername);
          } else {
            localStorage.removeItem('veil_username');
          }
          setCurrentUserRole(data.role || '');
        } else if (await staticTokenHasAdminAccess()) {`, 1)
	actions = strings.Replace(actions, `              const checkResp = await fetch('/api/version', { headers: authHeaders() });
              if (checkResp.ok) {
                const checkData = await checkResp.json();
                btn.disabled = false;
                output.textContent = veilT('version.backOnline', { details: JSON.stringify(checkData, null, 2) });
                return;
              }`, `              const checkResp = await fetch('/api/version', { headers: authHeaders(), cache: 'no-store' });
              if (checkResp.ok || checkResp.status === 401 || checkResp.status === 403) {
                let details = 'Authentication session reset after restart.';
                if (checkResp.ok) {
                  try {
                    details = JSON.stringify(await checkResp.json(), null, 2);
                  } catch (_) {}
                }
                btn.disabled = false;
                output.textContent = veilT('version.backOnline', { details });
                invalidateCurrentUserRoleRefresh();
                localStorage.removeItem('veil_csrf_token');
                localStorage.removeItem('veil_username');
                localStorage.removeItem('veil_user_role');
                setTimeout(() => window.location.reload(), 100);
                return;
              }`, 1)
	return actions
}
