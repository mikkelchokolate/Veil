package panel

// panelUsersReliabilityJS replaces the original Users actions with guarded
// variants. It keeps late list responses from overwriting newer state and
// prevents duplicate user/session mutations from repeated clicks.
func panelUsersReliabilityJS() string {
	return `
    let usersLoadGeneration = 0;
    let sessionsLoadGeneration = 0;
    let userMutationInFlight = false;

    function setUserMutationControlsDisabled(disabled) {
      document.querySelectorAll('#btn-save-user, [data-user-mutation="true"]').forEach((button) => {
        button.disabled = Boolean(disabled) || isViewerRole();
      });
    }

    async function withUserMutation(action) {
      if (userMutationInFlight) return null;
      userMutationInFlight = true;
      setUserMutationControlsDisabled(true);
      try {
        return await action();
      } finally {
        userMutationInFlight = false;
        setUserMutationControlsDisabled(false);
        applyViewerRoleGuard();
      }
    }

    loadUsers = async function() {
      const generation = ++usersLoadGeneration;
      const tbody = document.getElementById('users-table-body');
      if (!tbody) return;
      setUserTableMessage(tbody, 4, veilT('status.loadingPath', { path: '/api/users' }), 'var(--text-muted)');
      try {
        const response = await fetch('/api/users', { headers: authHeaders() });
        const text = await response.text();
        if (generation !== usersLoadGeneration) return;
        if (!response.ok) {
          const message = response.status === 403 ? veilT('status.accessDenied') : formatAPIError(text, response.status);
          setUserTableMessage(tbody, 4, message, response.status === 403 ? 'var(--text-muted)' : 'var(--accent-danger)');
          return;
        }
        const users = text ? JSON.parse(text) : [];
        if (!Array.isArray(users)) {
          setUserTableMessage(tbody, 4, 'Invalid users response.', 'var(--accent-danger)');
          return;
        }
        if (users.length === 0) {
          setUserTableMessage(tbody, 4, veilT('status.noUsers'), 'var(--text-muted)');
          return;
        }
        tbody.textContent = '';
        users.forEach((userInfo) => {
          const row = document.createElement('tr');
          const username = document.createElement('td');
          username.textContent = String(userInfo && userInfo.username || '');
          row.appendChild(username);
          const role = document.createElement('td');
          appendUserRoleBadge(role, userInfo && userInfo.role);
          row.appendChild(role);
          const locale = document.createElement('td');
          locale.textContent = String(userInfo && userInfo.locale || 'en');
          row.appendChild(locale);
          const actions = document.createElement('td');
          const edit = document.createElement('button');
          edit.type = 'button';
          edit.textContent = veilT('users.edit');
          edit.className = 'secondary';
          edit.dataset.adminOnly = 'true';
          edit.dataset.userMutation = 'true';
          edit.style.padding = '6px 12px';
          edit.style.fontSize = '0.75rem';
          edit.style.marginRight = '8px';
          edit.addEventListener('click', () => editUser(userInfo.username, userInfo.role, userInfo.locale));
          actions.appendChild(edit);
          const remove = document.createElement('button');
          remove.type = 'button';
          remove.textContent = veilT('action.delete');
          remove.className = 'danger';
          remove.dataset.adminOnly = 'true';
          remove.dataset.userMutation = 'true';
          remove.style.padding = '6px 12px';
          remove.style.fontSize = '0.75rem';
          remove.addEventListener('click', () => deleteUser(userInfo.username));
          actions.appendChild(remove);
          row.appendChild(actions);
          tbody.appendChild(row);
        });
        applyViewerRoleGuard();
      } catch (err) {
        if (generation === usersLoadGeneration) {
          setUserTableMessage(tbody, 4, veilT('status.requestFailed', { error: String(err) }), 'var(--accent-danger)');
        }
      }
    };

    loadSessions = async function() {
      const generation = ++sessionsLoadGeneration;
      const tbody = document.getElementById('sessions-table-body');
      if (!tbody) return;
      const loadButton = document.getElementById('btn-load-sessions');
      if (loadButton) loadButton.disabled = true;
      setUserTableMessage(tbody, 5, veilT('status.loadingSessions'), 'var(--text-muted)');
      try {
        const response = await fetch('/api/auth/sessions', { headers: authHeaders() });
        const text = await response.text();
        if (generation !== sessionsLoadGeneration) return;
        if (!response.ok) {
          setUserTableMessage(tbody, 5, formatAPIError(text, response.status), 'var(--accent-danger)');
          return;
        }
        const sessions = text ? JSON.parse(text) : [];
        if (!Array.isArray(sessions)) {
          setUserTableMessage(tbody, 5, 'Invalid sessions response.', 'var(--accent-danger)');
          return;
        }
        if (sessions.length === 0) {
          setUserTableMessage(tbody, 5, veilT('status.noSessions'), 'var(--text-muted)');
          return;
        }
        tbody.textContent = '';
        sessions.forEach((session) => {
          const row = document.createElement('tr');
          const user = document.createElement('td');
          user.textContent = String(session && session.username || '');
          row.appendChild(user);
          const role = document.createElement('td');
          appendUserRoleBadge(role, session && session.role);
          row.appendChild(role);
          const expires = document.createElement('td');
          expires.textContent = String(session && session.expiresAt || '');
          row.appendChild(expires);
          const current = document.createElement('td');
          current.textContent = session && session.current ? veilT('common.yes') : veilT('common.no');
          row.appendChild(current);
          const actions = document.createElement('td');
          const revoke = document.createElement('button');
          revoke.type = 'button';
          revoke.className = session && session.current ? 'secondary' : 'danger';
          revoke.dataset.adminOnly = 'true';
          revoke.dataset.userMutation = 'true';
          revoke.style.padding = '6px 12px';
          revoke.style.fontSize = '0.75rem';
          revoke.textContent = session && session.current ? veilT('users.revokeSelf') : veilT('users.revoke');
          revoke.addEventListener('click', () => revokeSession(session.id, Boolean(session.current)));
          actions.appendChild(revoke);
          row.appendChild(actions);
          tbody.appendChild(row);
        });
        applyViewerRoleGuard();
      } catch (err) {
        if (generation === sessionsLoadGeneration) {
          setUserTableMessage(tbody, 5, veilT('status.requestFailed', { error: String(err) }), 'var(--accent-danger)');
        }
      } finally {
        if (loadButton && generation === sessionsLoadGeneration) loadButton.disabled = isViewerRole();
      }
    };

    revokeSession = async function(id, isCurrent) {
      if (!confirm(isCurrent ? veilT('confirm.revokeCurrent') : veilT('confirm.revokeSession'))) return null;
      return withUserMutation(async () => {
        try {
          const response = await fetch('/api/auth/sessions', {
            method: 'DELETE',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ id })
          });
          const text = await response.text();
          if (!response.ok) {
            alert(formatAPIError(text, response.status));
            return null;
          }
          if (isCurrent) {
            clearStoredPanelIdentity();
            window.location.reload();
            return true;
          }
          await loadSessions();
          return true;
        } catch (err) {
          alert(veilT('users.sessionRevokeFailed', { error: String(err) }));
          return null;
        }
      });
    };

    saveUser = async function(event) {
      if (event) event.preventDefault();
      const form = document.getElementById('user-form');
      if (form && !form.checkValidity()) {
        form.reportValidity();
        return null;
      }
      const isEdit = document.getElementById('user-is-edit').value === 'true';
      const username = document.getElementById('user-username').value.trim();
      const password = document.getElementById('user-password').value;
      const role = document.getElementById('user-role').value;
      const locale = document.getElementById('user-locale').value;
      const output = document.getElementById('user-output');
      if (!username) {
        output.textContent = 'Username is required.';
        return null;
      }
      const payload = { role, locale };
      if (password) {
        payload.password = password;
      } else if (!isEdit) {
        output.textContent = veilT('users.passwordRequired');
        return null;
      }
      if (!isEdit) payload.username = username;
      const path = isEdit ? '/api/users/' + encodeURIComponent(username) : '/api/users';
      const method = isEdit ? 'PUT' : 'POST';
      return withUserMutation(async () => {
        output.textContent = isEdit ? veilT('users.updating') : veilT('users.creating');
        try {
          const response = await fetch(path, {
            method,
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify(payload)
          });
          const text = await response.text();
          if (!response.ok) {
            output.textContent = veilT('common.error', { error: formatAPIError(text, response.status) });
            return null;
          }
          output.textContent = isEdit ? veilT('users.updated') : veilT('users.created');
          if (isEdit && username === currentPanelUsername()) {
            clearStoredPanelIdentity();
            window.location.reload();
            return true;
          }
          cancelUserEdit();
          await loadUsers();
          return true;
        } catch (err) {
          output.textContent = veilT('status.requestFailed', { error: String(err) });
          return null;
        }
      });
    };

    deleteUser = async function(username) {
      if (!confirm(veilT('confirm.deleteUser', { username }))) return null;
      const output = document.getElementById('user-output');
      return withUserMutation(async () => {
        output.textContent = veilT('users.deleting');
        try {
          const response = await fetch('/api/users/' + encodeURIComponent(username), {
            method: 'DELETE',
            headers: requestHeaders()
          });
          const text = await response.text();
          if (!response.ok) {
            output.textContent = veilT('users.deleteFailed', { error: formatAPIError(text, response.status) });
            return null;
          }
          output.textContent = veilT('users.deleted');
          if (username === currentPanelUsername()) {
            clearStoredPanelIdentity();
            window.location.reload();
            return true;
          }
          cancelUserEdit();
          await loadUsers();
          return true;
        } catch (err) {
          output.textContent = veilT('status.requestFailed', { error: String(err) });
          return null;
        }
      });
    };
`
}
