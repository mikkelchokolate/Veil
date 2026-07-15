package panel

const panelIntroActionsPlaceholder = "__VEIL_PANEL_INTRO_ACTIONS__"

func panelIntroActionsJS() string {
	return `    const tokenInput = document.getElementById('api-token');
    tokenInput.value = localStorage.getItem('veil_api_token') || '';
    tokenInput.addEventListener('input', () => {
      if (tokenInput.value) {
        localStorage.setItem('veil_api_token', tokenInput.value);
      } else {
        localStorage.removeItem('veil_api_token');
      }
      scheduleCurrentUserRoleRefresh();
    });

    // Toggle API Token Visibility
    const toggleBtn = document.getElementById('toggle-api-token-visibility');
    if (toggleBtn) {
      toggleBtn.addEventListener('click', () => {
        const isPassword = tokenInput.getAttribute('type') === 'password';
        tokenInput.setAttribute('type', isPassword ? 'text' : 'password');
        toggleBtn.innerHTML = isPassword 
          ? '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.542-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l18 18"/></svg>'
          : '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>';
      });
    }

    function authHeaders() {
      const headers = {};
      const token = localStorage.getItem('veil_api_token') || '';
      if (token) {
        headers['X-Veil-Token'] = token;
      }
      const csrf = window.veil_csrf_token || localStorage.getItem('veil_csrf_token') || '';
      if (csrf) {
        headers['X-CSRF-Token'] = csrf;
      }
      return headers;
    }

    function requestHeaders(extra) {
      return Object.assign({}, extra || {}, authHeaders());
    }

    function formatAPIError(text, status) {
      if (!text) return 'HTTP ' + status;
      try {
        const data = JSON.parse(text);
        if (data && data.error && data.error.message) {
          return data.error.message;
        }
        if (data && data.message) {
          return data.message;
        }
      } catch (_) {}
      return text;
    }

    function currentUserRole() {
      return window.veil_user_role || localStorage.getItem('veil_user_role') || '';
    }

    function setCurrentUserRole(role) {
      window.veil_user_role = role || '';
      if (role) {
        localStorage.setItem('veil_user_role', role);
      } else {
        localStorage.removeItem('veil_user_role');
      }
    }

    function isViewerRole() {
      // Unknown, stale, or failed role resolution must remain read-only. Only an
      // explicitly authenticated administrator may use mutating controls.
      return currentUserRole() !== 'admin';
    }

    const adminOnlyControlIds = [
      'update-version',
      'add-inbound-btn',
      'save-settings',
      'save-inbound',
      'delete-inbound',
      'add-routing-rule-btn',
      'apply-routing-preset',
      'save-routing-rule',
      'delete-routing-rule',
      'save-warp-config',
      'apply-staged-files',
      'apply-live-configs',
      'reload-services',
      'btn-save-user',
      'btn-cancel-user-edit',
      'btn-load-sessions',
      'btn-generate-api-token',
      'btn-copy-generated-api-token'
    ];

    const adminOnlyFormIds = [
      'settings-form',
      'inbound-form',
      'routing-rule-form',
      'warp-form',
      'user-form'
    ];

    function blockViewerAdminAction(event) {
      if (!isViewerRole()) {
        return;
      }
      event.preventDefault();
      event.stopImmediatePropagation();
      alert(veilT('role.viewerReadOnly'));
    }

    function wireViewerGuard(el) {
      if (!el || el.dataset.viewerGuardBound === 'true') {
        return;
      }
      el.addEventListener('click', blockViewerAdminAction, true);
      el.dataset.viewerGuardBound = 'true';
    }

    function applyViewerRoleGuard() {
      const viewer = isViewerRole();
      const controls = [];
      adminOnlyControlIds.forEach((id) => {
        const el = document.getElementById(id);
        if (el) {
          controls.push(el);
        }
      });
      document.querySelectorAll('[data-admin-only="true"], button[id^="restart-"]').forEach((el) => controls.push(el));
      controls.forEach((el) => {
        wireViewerGuard(el);
        if (viewer) {
          if (el.dataset.viewerGuardDisabled !== 'true') {
            el.dataset.viewerGuardWasDisabled = el.disabled ? 'true' : 'false';
            el.dataset.viewerGuardDisabled = 'true';
          }
          el.disabled = true;
        } else if (el.dataset.viewerGuardDisabled === 'true') {
          el.disabled = el.dataset.viewerGuardWasDisabled === 'true';
          delete el.dataset.viewerGuardWasDisabled;
          delete el.dataset.viewerGuardDisabled;
        }
        el.title = viewer ? veilT('role.viewerReadOnlyShort') : '';
      });
      adminOnlyFormIds.forEach((id) => {
        const form = document.getElementById(id);
        if (!form || form.dataset.viewerGuardSubmitBound === 'true') {
          return;
        }
        form.addEventListener('submit', blockViewerAdminAction, true);
        form.dataset.viewerGuardSubmitBound = 'true';
      });
      document.body.dataset.veilRole = currentUserRole();
    }

    async function staticTokenHasAdminAccess() {
      if (!localStorage.getItem('veil_api_token')) return false;
      try {
        const response = await fetch('/api/version', { headers: authHeaders() });
        return response.ok;
      } catch (_) {
        return false;
      }
    }

    let currentUserRoleRefreshGeneration = 0;
    let currentUserRoleRefreshController = null;
    let currentUserRoleRefreshTimer = null;

    function invalidateCurrentUserRoleRefresh() {
      currentUserRoleRefreshGeneration += 1;
      if (currentUserRoleRefreshController) {
        currentUserRoleRefreshController.abort();
        currentUserRoleRefreshController = null;
      }
      if (currentUserRoleRefreshTimer) {
        clearTimeout(currentUserRoleRefreshTimer);
        currentUserRoleRefreshTimer = null;
      }
      setCurrentUserRole('');
      applyViewerRoleGuard();
    }

    function scheduleCurrentUserRoleRefresh() {
      invalidateCurrentUserRoleRefresh();
      currentUserRoleRefreshTimer = setTimeout(() => {
        currentUserRoleRefreshTimer = null;
        refreshCurrentUserRole();
      }, 250);
    }

    async function refreshCurrentUserRole() {
      const generation = ++currentUserRoleRefreshGeneration;
      if (currentUserRoleRefreshController) currentUserRoleRefreshController.abort();
      const controller = new AbortController();
      currentUserRoleRefreshController = controller;
      const tokenSnapshot = localStorage.getItem('veil_api_token') || '';

      // Clear cached authority before checking it. This prevents a role from a
      // revoked session or replaced API token from enabling controls briefly.
      setCurrentUserRole('');
      applyViewerRoleGuard();
      try {
        const response = await fetch('/api/auth/status', { headers: authHeaders(), signal: controller.signal });
        if (generation !== currentUserRoleRefreshGeneration || tokenSnapshot !== (localStorage.getItem('veil_api_token') || '')) return;
        if (!response.ok) return;
        const data = await response.json();
        if (generation !== currentUserRoleRefreshGeneration || tokenSnapshot !== (localStorage.getItem('veil_api_token') || '')) return;
        if (data && data.authenticated) {
          setCurrentUserRole(data.role || '');
        } else if (await staticTokenHasAdminAccess()) {
          if (generation !== currentUserRoleRefreshGeneration || tokenSnapshot !== (localStorage.getItem('veil_api_token') || '')) return;
          setCurrentUserRole('admin');
        }
      } catch (error) {
        if (!error || error.name !== 'AbortError') {
          if (generation === currentUserRoleRefreshGeneration) setCurrentUserRole('');
        }
      } finally {
        if (currentUserRoleRefreshController === controller) {
          currentUserRoleRefreshController = null;
          if (generation === currentUserRoleRefreshGeneration) applyViewerRoleGuard();
        }
      }
    }

    const viewerGuardObserver = new MutationObserver(() => applyViewerRoleGuard());
    viewerGuardObserver.observe(document.body, { childList: true, subtree: true });
    applyViewerRoleGuard();
    refreshCurrentUserRole();

    async function loadJSON(path, outputId, options) {
      const output = document.getElementById(outputId);
      output.textContent = veilT('status.loadingPath', { path });
      const requestOptions = Object.assign({}, options || {});
      requestOptions.headers = requestHeaders(requestOptions.headers || {});
      try {
        const response = await fetch(path, requestOptions);
        const text = await response.text();
        if (!response.ok) {
          output.textContent = formatAPIError(text, response.status);
          return null;
        }
        if (!text) {
          output.textContent = veilT('common.ok');
          return { success: true };
        }
        const parsed = JSON.parse(text);
        output.textContent = JSON.stringify(parsed, null, 2);
        return parsed;
      } catch (err) {
        output.textContent = String(err);
        return null;
      }
    }

    document.getElementById('profile-preview-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const domain = document.getElementById('profile-domain').value.trim();
      const email = document.getElementById('profile-email').value.trim();
      const panelAccess = document.getElementById('profile-panel-access').value;
      if (panelAccess === 'caddy' && (!domain || !email)) {
        document.getElementById('profile-preview-output').textContent = veilT('installPreview.caddyRequired');
        return;
      }
      await loadJSON('/api/profiles/ru-recommended/preview', 'profile-preview-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain, email, panelAccess })
      });
    });

    document.getElementById('load-version').addEventListener('click', async () => {
      await loadJSON('/api/version', 'version-output');
    });

    document.getElementById('update-version').addEventListener('click', async () => {
      const btn = document.getElementById('update-version');
      const output = document.getElementById('version-output');
      btn.disabled = true;
      output.textContent = veilT('version.startingUpdate');
      
      try {
        const response = await fetch('/api/version/update', {
          method: 'POST',
          headers: requestHeaders({ 'Content-Type': 'application/json' })
        });
        const text = await response.text();
        let data = null;
        try { data = JSON.parse(text); } catch (_) {}
        
        if (!response.ok) {
          btn.disabled = false;
          if (data && data.log) {
            output.textContent = veilT('version.updateFailed', { details: data.log + "\n\n" + veilT('common.error', { error: data.message || veilT('status.unknownError') }) });
          } else {
            output.textContent = veilT('version.updateFailed', { details: formatAPIError(text, response.status) });
          }
          return;
        }
        
        output.textContent = (data && data.log ? data.log + "\n\n" : "") + veilT('version.updateStaged');
        
        setTimeout(async () => {
          const maxAttempts = 20;
          for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
            output.textContent = veilT('version.waitingRestart', { attempt, max: maxAttempts });
            try {
              const checkResp = await fetch('/api/version', { headers: authHeaders() });
              if (checkResp.ok) {
                const checkData = await checkResp.json();
                btn.disabled = false;
                output.textContent = veilT('version.backOnline', { details: JSON.stringify(checkData, null, 2) });
                return;
              }
            } catch (_) {
            }
            if (attempt < maxAttempts) {
              await new Promise((resolve) => setTimeout(resolve, 2000));
            }
          }
          btn.disabled = false;
          output.textContent = veilT('version.restartSlow');
        }, 3000);
        
      } catch (err) {
        btn.disabled = false;
        output.textContent = veilT('status.requestFailed', { error: String(err) });
      }
    });

    const logoutBtn = document.getElementById('btn-logout');
    if (logoutBtn) {
      logoutBtn.addEventListener('click', async () => {
        try {
          await fetch('/api/auth/logout', {
            method: 'POST',
            headers: requestHeaders()
          });
        } catch (_) {}
        invalidateCurrentUserRoleRefresh();
        localStorage.removeItem('veil_api_token');
        localStorage.removeItem('veil_csrf_token');
        localStorage.removeItem('veil_username');
        localStorage.removeItem('veil_user_role');
        tokenInput.value = '';
        window.location.reload();
      });
    }`
}
