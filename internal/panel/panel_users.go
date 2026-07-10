package panel

const panelUsersCardPlaceholder = "__VEIL_PANEL_USERS_CARD__"
const panelUsersActionsPlaceholder = "__VEIL_PANEL_USERS_ACTIONS__"

func panelUsersCardHTML() string {
	return `
<div class="card">
  <h2><span class="pulse-static"></span>&nbsp;User Management</h2>
  <p class="hint" style="margin-bottom: 20px;">Manage administrative and viewer credentials. Viewer accounts can view diagnostics, settings, and stats, but cannot add/delete inbounds, modify routing rules, or apply changes.</p>
  
  <div class="table-container">
    <table>
      <thead>
        <tr>
          <th>Username</th>
          <th>Role</th>
          <th>Locale</th>
          <th style="width: 150px;">Actions</th>
        </tr>
      </thead>
      <tbody id="users-table-body">
        <tr>
          <td colspan="4" style="text-align: center; color: var(--text-muted);">Loading users...</td>
        </tr>
      </tbody>
    </table>
  </div>
</div>

<div class="card">
  <h2>Active Sessions</h2>
  <p class="hint" style="margin-bottom: 20px;">Review browser sessions and revoke stale operator access. The current session is marked so it is harder to lock yourself out accidentally.</p>
  <div class="actions">
    <button type="button" id="btn-load-sessions">Load sessions</button>
  </div>
  <div class="table-container">
    <table>
      <thead>
        <tr>
          <th>User</th>
          <th>Role</th>
          <th>Expires</th>
          <th>Current</th>
          <th style="width: 120px;">Actions</th>
        </tr>
      </thead>
      <tbody id="sessions-table-body">
        <tr>
          <td colspan="5" style="text-align: center; color: var(--text-muted);">Not loaded</td>
        </tr>
      </tbody>
    </table>
  </div>
</div>

<div class="card">
  <h2>API Token Rotation</h2>
  <p class="hint">Generate a replacement API token, update <code>VEIL_API_TOKEN</code> in your service environment or restart command, then restart the Panel. Existing browser sessions are managed above.</p>
  <div class="actions">
    <button type="button" id="btn-generate-api-token">Generate token</button>
    <button type="button" id="btn-copy-generated-api-token" class="secondary">Copy generated token</button>
  </div>
  <pre id="token-rotation-output" role="status" aria-live="polite">No generated token</pre>
</div>

<div class="card">
  <h2 id="user-form-title">Add New User</h2>
  <form id="user-form">
    <input type="hidden" id="user-is-edit" value="false">
    <div class="form-grid">
      <div>
        <label for="user-username">Username</label>
        <input type="text" id="user-username" autocomplete="off" placeholder="Username" required>
      </div>
      <div>
        <label for="user-password">Password</label>
        <input type="password" id="user-password" autocomplete="new-password" placeholder="Password" required>
      </div>
      <div>
        <label for="user-role">Role</label>
        <select id="user-role">
          <option value="admin">Administrator (Read-Write)</option>
          <option value="viewer">Viewer (Read-Only)</option>
        </select>
      </div>
      <div>
        <label for="user-locale">Locale</label>
        <select id="user-locale">
          <option value="en">English</option>
          <option value="ru">Русский</option>
        </select>
      </div>
    </div>
    <div class="actions">
      <button type="submit" id="btn-save-user">Create User</button>
      <button type="button" class="secondary" id="btn-cancel-user-edit" style="display: none;">Cancel</button>
    </div>
  </form>
  <div class="veil-output-container" style="margin-top: 16px;">
    <div class="veil-output-label">Status Output</div>
    <pre id="user-output" class="veil-output-pre" role="status" aria-live="polite">Ready</pre>
  </div>
</div>
`
}

func panelUsersActionsJS() string {
	return `
    function setUserTableMessage(tbody, colspan, text, color) {
      tbody.textContent = '';
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = colspan;
      cell.style.textAlign = 'center';
      cell.style.color = color;
      cell.textContent = text;
      row.appendChild(cell);
      tbody.appendChild(row);
    }

    function appendUserRoleBadge(cell, role) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = String(role || '');
      cell.appendChild(badge);
    }

    function currentPanelUsername() {
      return localStorage.getItem('veil_username') || '';
    }

    function clearStoredPanelIdentity() {
      localStorage.removeItem('veil_csrf_token');
      localStorage.removeItem('veil_username');
      localStorage.removeItem('veil_user_role');
    }

    async function loadUsers() {
      const tbody = document.getElementById('users-table-body');
      if (!tbody) return;
      try {
        const response = await fetch('/api/users', { headers: authHeaders() });
        if (!response.ok) {
          if (response.status === 403) {
            setUserTableMessage(tbody, 4, veilT('status.accessDenied'), 'var(--text-muted)');
            return;
          }
          const text = await response.text();
          setUserTableMessage(tbody, 4, veilT('common.error', { error: text }), 'var(--accent-danger)');
          return;
        }
        const users = await response.json();
        if (!users || users.length === 0) {
          setUserTableMessage(tbody, 4, veilT('status.noUsers'), 'var(--text-muted)');
          return;
        }
        tbody.textContent = '';
        users.forEach(u => {
          const tr = document.createElement('tr');
          
          const tdUser = document.createElement('td');
          tdUser.textContent = u.username;
          tr.appendChild(tdUser);
          
          const tdRole = document.createElement('td');
          appendUserRoleBadge(tdRole, u.role);
          tr.appendChild(tdRole);

          const tdLocale = document.createElement('td');
          tdLocale.textContent = u.locale || 'en';
          tr.appendChild(tdLocale);
          
          const tdActions = document.createElement('td');
          
          const editBtn = document.createElement('button');
          editBtn.textContent = veilT('users.edit');
          editBtn.className = 'secondary';
          editBtn.dataset.adminOnly = 'true';
          editBtn.style.padding = '6px 12px';
          editBtn.style.fontSize = '0.75rem';
          editBtn.style.marginRight = '8px';
          editBtn.addEventListener('click', () => editUser(u.username, u.role, u.locale));
          tdActions.appendChild(editBtn);
          
          const deleteBtn = document.createElement('button');
          deleteBtn.textContent = veilT('action.delete');
          deleteBtn.className = 'danger';
          deleteBtn.dataset.adminOnly = 'true';
          deleteBtn.style.padding = '6px 12px';
          deleteBtn.style.fontSize = '0.75rem';
          deleteBtn.addEventListener('click', () => deleteUser(u.username));
          tdActions.appendChild(deleteBtn);
          
          tr.appendChild(tdActions);
          tbody.appendChild(tr);
        });
        applyViewerRoleGuard();
      } catch (err) {
        setUserTableMessage(tbody, 4, veilT('status.requestFailed', { error: String(err) }), 'var(--accent-danger)');
      }
    }

    async function loadSessions() {
      const tbody = document.getElementById('sessions-table-body');
      if (!tbody) return;
      setUserTableMessage(tbody, 5, veilT('status.loadingSessions'), 'var(--text-muted)');
      try {
        const response = await fetch('/api/auth/sessions', { headers: authHeaders() });
        const text = await response.text();
        if (!response.ok) {
          setUserTableMessage(tbody, 5, text || ('HTTP ' + response.status), 'var(--accent-danger)');
          return;
        }
        const sessions = text ? JSON.parse(text) : [];
        if (!sessions.length) {
          setUserTableMessage(tbody, 5, veilT('status.noSessions'), 'var(--text-muted)');
          return;
        }
        tbody.textContent = '';
        sessions.forEach((session) => {
          const tr = document.createElement('tr');
          const user = document.createElement('td');
          user.textContent = session.username;
          tr.appendChild(user);
          const role = document.createElement('td');
          appendUserRoleBadge(role, session.role);
          tr.appendChild(role);
          const expires = document.createElement('td');
          expires.textContent = session.expiresAt || '';
          tr.appendChild(expires);
          const current = document.createElement('td');
          current.textContent = session.current ? veilT('common.yes') : veilT('common.no');
          tr.appendChild(current);
          const actions = document.createElement('td');
          const revoke = document.createElement('button');
          revoke.type = 'button';
          revoke.className = session.current ? 'secondary' : 'danger';
          revoke.dataset.adminOnly = 'true';
          revoke.style.padding = '6px 12px';
          revoke.style.fontSize = '0.75rem';
          revoke.textContent = session.current ? veilT('users.revokeSelf') : veilT('users.revoke');
          revoke.addEventListener('click', () => revokeSession(session.id, Boolean(session.current)));
          actions.appendChild(revoke);
          tr.appendChild(actions);
          tbody.appendChild(tr);
        });
        applyViewerRoleGuard();
      } catch (err) {
        setUserTableMessage(tbody, 5, veilT('status.requestFailed', { error: String(err) }), 'var(--accent-danger)');
      }
    }

    async function revokeSession(id, isCurrent) {
      if (!confirm(isCurrent ? veilT('confirm.revokeCurrent') : veilT('confirm.revokeSession'))) {
        return;
      }
      try {
        const response = await fetch('/api/auth/sessions', {
          method: 'DELETE',
          headers: requestHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify({ id })
        });
        if (!response.ok) {
          alert(await response.text());
          return;
        }
        if (isCurrent) {
          clearStoredPanelIdentity();
          window.location.reload();
          return;
        }
        await loadSessions();
      } catch (err) {
        alert(veilT('users.sessionRevokeFailed', { error: String(err) }));
      }
    }

    function generateReplacementAPIToken() {
      const bytes = new Uint8Array(32);
      crypto.getRandomValues(bytes);
      const token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
      const output = document.getElementById('token-rotation-output');
      output.dataset.token = token;
      output.textContent = veilT('users.generatedToken', { token });
    }

    async function copyGeneratedAPIToken() {
      const output = document.getElementById('token-rotation-output');
      const token = output.dataset.token || '';
      if (!token) {
        output.textContent = veilT('users.generateTokenFirst');
        return;
      }
      try {
        await navigator.clipboard.writeText(token);
        output.textContent = output.textContent + '\n\n' + veilT('users.tokenCopied');
      } catch (err) {
        output.textContent = output.textContent + '\n\n' + veilT('status.copyFailed', { error: String(err) });
      }
    }

    function editUser(username, role, locale) {
      document.getElementById('user-is-edit').value = 'true';
      const usernameInput = document.getElementById('user-username');
      usernameInput.value = username;
      usernameInput.disabled = true;
      
      const pwdInput = document.getElementById('user-password');
      pwdInput.value = '';
      pwdInput.placeholder = 'Leave blank to keep current';
      pwdInput.required = false;
      
      document.getElementById('user-role').value = role;
      document.getElementById('user-locale').value = locale || 'en';
      document.getElementById('user-form-title').textContent = veilT('users.editTitle', { username });
      document.getElementById('btn-save-user').textContent = veilT('users.saveChanges');
      document.getElementById('btn-cancel-user-edit').style.display = 'inline-flex';
    }

    function cancelUserEdit() {
      document.getElementById('user-is-edit').value = 'false';
      const usernameInput = document.getElementById('user-username');
      usernameInput.value = '';
      usernameInput.disabled = false;
      
      const pwdInput = document.getElementById('user-password');
      pwdInput.value = '';
      pwdInput.placeholder = 'Password';
      pwdInput.required = true;
      
      document.getElementById('user-role').value = 'admin';
      document.getElementById('user-locale').value = 'en';
      document.getElementById('user-form-title').textContent = veilT('users.addTitle');
      document.getElementById('btn-save-user').textContent = veilT('users.create');
      document.getElementById('btn-cancel-user-edit').style.display = 'none';
    }

    async function saveUser(event) {
      if (event) event.preventDefault();
      const isEdit = document.getElementById('user-is-edit').value === 'true';
      const username = document.getElementById('user-username').value.trim();
      const password = document.getElementById('user-password').value;
      const role = document.getElementById('user-role').value;
      const locale = document.getElementById('user-locale').value;
      const output = document.getElementById('user-output');
      output.textContent = isEdit ? veilT('users.updating') : veilT('users.creating');
      
      const payload = { role, locale };
      if (password) {
        payload.password = password;
      } else if (!isEdit) {
        output.textContent = veilT('users.passwordRequired');
        return;
      }
      
      const path = isEdit ? '/api/users/' + encodeURIComponent(username) : '/api/users';
      const method = isEdit ? 'PUT' : 'POST';
      
      if (!isEdit) {
        payload.username = username;
      }
      
      try {
        const response = await fetch(path, {
          method: method,
          headers: requestHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify(payload)
        });
        const text = await response.text();
        if (!response.ok) {
          let msg = text;
          try {
            const data = JSON.parse(text);
            if (data && data.message) msg = data.message;
          } catch (_) {}
          output.textContent = veilT('common.error', { error: msg });
          return;
        }
        output.textContent = isEdit ? veilT('users.updated') : veilT('users.created');
        if (isEdit && username === currentPanelUsername()) {
          clearStoredPanelIdentity();
          window.location.reload();
          return;
        }
        cancelUserEdit();
        await loadUsers();
      } catch (err) {
        output.textContent = veilT('status.requestFailed', { error: String(err) });
      }
    }

    async function deleteUser(username) {
      if (!confirm(veilT('confirm.deleteUser', { username }))) {
        return;
      }
      const output = document.getElementById('user-output');
      output.textContent = veilT('users.deleting');
      try {
        const response = await fetch('/api/users/' + encodeURIComponent(username), {
          method: 'DELETE',
          headers: requestHeaders()
        });
        if (!response.ok) {
          const text = await response.text();
          let msg = text;
          try {
            const data = JSON.parse(text);
            if (data && data.message) msg = data.message;
          } catch (_) {}
          output.textContent = veilT('users.deleteFailed', { error: msg });
          return;
        }
        output.textContent = veilT('users.deleted');
        if (username === currentPanelUsername()) {
          clearStoredPanelIdentity();
          window.location.reload();
          return;
        }
        cancelUserEdit();
        await loadUsers();
      } catch (err) {
        output.textContent = veilT('status.requestFailed', { error: String(err) });
      }
    }
`
}
