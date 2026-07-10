package panel

const panelBackupsCardPlaceholder = "__VEIL_PANEL_BACKUPS_CARD__"
const panelBackupsActionsPlaceholder = "__VEIL_PANEL_BACKUPS_ACTIONS__"

func panelBackupsCardHTML() string {
	return `
<div class="card">
  <h2><span class="pulse-static"></span>&nbsp;Disaster Recovery</h2>
  <p class="hint">Archives are encrypted with the server-side scheduled backup passphrase. Configure it once with <code>sudo veil backup schedule enable</code>; the browser never receives the passphrase. Backup access requires the admin role.</p>
  <div class="form-grid">
    <div>
      <label for="backup-daily">Daily copies</label>
      <input id="backup-daily" type="number" min="0" max="365" value="7" required>
    </div>
    <div>
      <label for="backup-weekly">Weekly copies</label>
      <input id="backup-weekly" type="number" min="0" max="104" value="4" required>
    </div>
    <div>
      <label for="backup-monthly">Monthly copies</label>
      <input id="backup-monthly" type="number" min="0" max="120" value="12" required>
    </div>
  </div>
  <div class="actions">
    <button type="button" id="btn-create-backup" data-admin-only="true">Create encrypted backup</button>
    <button type="button" id="btn-load-backups" class="secondary" data-admin-only="true">Refresh</button>
    <button type="button" id="btn-prune-backups" class="danger" data-admin-only="true">Apply retention</button>
  </div>
  <div class="table-container">
    <table>
      <thead>
        <tr>
          <th>Created</th>
          <th>Archive</th>
          <th>Size</th>
          <th>Encrypted</th>
          <th style="width: 260px;">Actions</th>
        </tr>
      </thead>
      <tbody id="backups-table-body">
        <tr><td colspan="5" style="text-align: center; color: var(--text-muted);">Not loaded</td></tr>
      </tbody>
    </table>
  </div>
  <pre id="backup-output" role="status" aria-live="polite">Ready</pre>
</div>
`
}

func panelBackupsActionsJS() string {
	return `
    let backupOperationInFlight = false;
    let backupLoadGeneration = 0;
    let backupRestorePollGeneration = 0;

    function backupRetentionValue(id, label) {
      const input = document.getElementById(id);
      const raw = input ? input.value.trim() : '';
      if (!input || raw === '' || !input.checkValidity()) {
        if (input) input.reportValidity();
        throw new Error(label + ' retention must be a whole number within the allowed range.');
      }
      const value = Number(raw);
      if (!Number.isInteger(value)) {
        input.setCustomValidity('Enter a whole number.');
        input.reportValidity();
        input.setCustomValidity('');
        throw new Error(label + ' retention must be a whole number.');
      }
      return value;
    }

    function backupRetention() {
      return {
        daily: backupRetentionValue('backup-daily', 'Daily'),
        weekly: backupRetentionValue('backup-weekly', 'Weekly'),
        monthly: backupRetentionValue('backup-monthly', 'Monthly')
      };
    }

    function setBackupOutput(value) {
      const output = document.getElementById('backup-output');
      if (output) output.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
    }

    function parseBackupResponse(text) {
      if (!text) return { success: true };
      try {
        return JSON.parse(text);
      } catch (_) {
        return text;
      }
    }

    function renderBackupTableMessage(message, color) {
      const tbody = document.getElementById('backups-table-body');
      if (!tbody) return;
      tbody.textContent = '';
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 5;
      cell.style.textAlign = 'center';
      cell.style.color = color || 'var(--text-muted)';
      cell.textContent = String(message || '');
      row.appendChild(cell);
      tbody.appendChild(row);
    }

    function setBackupControlsDisabled(disabled) {
      document.querySelectorAll('#btn-create-backup, #btn-prune-backups, [data-backup-action="true"]').forEach((button) => {
        button.disabled = Boolean(disabled) || isViewerRole();
      });
    }

    async function withBackupOperation(action) {
      if (backupOperationInFlight) return null;
      backupOperationInFlight = true;
      setBackupControlsDisabled(true);
      try {
        return await action();
      } finally {
        backupOperationInFlight = false;
        setBackupControlsDisabled(false);
        applyViewerRoleGuard();
      }
    }

    async function loadBackups() {
      const tbody = document.getElementById('backups-table-body');
      if (!tbody) return;
      const generation = ++backupLoadGeneration;
      if (isViewerRole()) {
        renderBackupTableMessage('Backup access requires the admin role.');
        applyViewerRoleGuard();
        return;
      }
      renderBackupTableMessage(veilT('backups.loading'));
      const refreshButton = document.getElementById('btn-load-backups');
      if (refreshButton) refreshButton.disabled = true;
      try {
        const response = await fetch('/api/backups', { headers: authHeaders() });
        const text = await response.text();
        if (generation !== backupLoadGeneration) return;
        if (!response.ok) {
          renderBackupTableMessage(formatAPIError(text, response.status), 'var(--accent-danger)');
          return;
        }
        const backups = text ? JSON.parse(text) : [];
        if (!Array.isArray(backups) || backups.length === 0) {
          renderBackupTableMessage(veilT('backups.empty'));
          return;
        }
        tbody.textContent = '';
        backups.forEach((item) => {
          const row = document.createElement('tr');
          [item.createdAt || '', item.name || '', String(item.size || 0), item.encrypted ? 'yes' : 'no'].forEach((value) => {
            const cell = document.createElement('td');
            cell.textContent = value;
            row.appendChild(cell);
          });
          const actions = document.createElement('td');
          const verify = document.createElement('button');
          verify.type = 'button';
          verify.className = 'secondary';
          verify.dataset.adminOnly = 'true';
          verify.dataset.backupAction = 'true';
          verify.textContent = veilT('backups.verify');
          verify.addEventListener('click', () => verifyBackup(item.name));
          actions.appendChild(verify);
          const download = document.createElement('button');
          download.type = 'button';
          download.className = 'secondary';
          download.dataset.adminOnly = 'true';
          download.dataset.backupAction = 'true';
          download.textContent = veilT('action.download');
          download.addEventListener('click', () => downloadBackup(item.name));
          actions.appendChild(download);
          const restore = document.createElement('button');
          restore.type = 'button';
          restore.className = 'danger';
          restore.dataset.adminOnly = 'true';
          restore.dataset.backupAction = 'true';
          restore.textContent = veilT('action.restore');
          restore.addEventListener('click', () => restoreBackup(item.name));
          actions.appendChild(restore);
          row.appendChild(actions);
          tbody.appendChild(row);
        });
        applyViewerRoleGuard();
      } catch (err) {
        if (generation === backupLoadGeneration) {
          const message = veilT('status.loadFailed', { error: String(err) });
          renderBackupTableMessage(message, 'var(--accent-danger)');
          setBackupOutput(message);
        }
      } finally {
        if (refreshButton && generation === backupLoadGeneration) refreshButton.disabled = isViewerRole();
      }
    }

    async function createBackup() {
      let retention;
      try {
        retention = backupRetention();
      } catch (err) {
        setBackupOutput(String(err));
        return null;
      }
      return withBackupOperation(async () => {
        setBackupOutput(veilT('backups.creating'));
        try {
          const response = await fetch('/api/backups', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify(Object.assign({ prune: true }, retention))
          });
          const text = await response.text();
          if (!response.ok) {
            setBackupOutput(formatAPIError(text, response.status));
            return null;
          }
          setBackupOutput(parseBackupResponse(text));
          await loadBackups();
          return true;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return null;
        }
      });
    }

    async function pruneBackups() {
      let retention;
      try {
        retention = backupRetention();
      } catch (err) {
        setBackupOutput(String(err));
        return null;
      }
      if (!confirm(veilT('confirm.pruneBackups'))) return null;
      return withBackupOperation(async () => {
        try {
          const response = await fetch('/api/backups/prune', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify(retention)
          });
          const text = await response.text();
          if (!response.ok) {
            setBackupOutput(formatAPIError(text, response.status));
            return null;
          }
          setBackupOutput(parseBackupResponse(text));
          await loadBackups();
          return true;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return null;
        }
      });
    }

    async function verifyBackup(name) {
      return withBackupOperation(async () => {
        setBackupOutput(veilT('backups.verifying', { name }));
        try {
          const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/verify', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: '{}'
          });
          const text = await response.text();
          setBackupOutput(response.ok ? parseBackupResponse(text) : formatAPIError(text, response.status));
          return response.ok;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return null;
        }
      });
    }

    async function downloadBackup(name) {
      return withBackupOperation(async () => {
        try {
          const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/download', { headers: authHeaders() });
          if (!response.ok) {
            setBackupOutput(formatAPIError(await response.text(), response.status));
            return null;
          }
          const blob = await response.blob();
          const url = URL.createObjectURL(blob);
          const link = document.createElement('a');
          link.href = url;
          link.download = name;
          document.body.appendChild(link);
          link.click();
          link.remove();
          setTimeout(() => URL.revokeObjectURL(url), 1000);
          return true;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return null;
        }
      });
    }

    async function restoreBackup(name) {
      const typed = prompt(veilT('confirm.restoreBackupPrompt', { name }));
      if (typed !== name) {
        setBackupOutput(veilT('status.restoreCancelled'));
        return null;
      }
      return withBackupOperation(async () => {
        try {
          const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/restore', {
            method: 'POST',
            headers: requestHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ confirm: true })
          });
          const text = await response.text();
          if (!response.ok) {
            setBackupOutput(formatAPIError(text, response.status));
            return null;
          }
          const job = JSON.parse(text);
          if (!job || !job.id) {
            setBackupOutput('Backup restore response did not include a job id.');
            return null;
          }
          setBackupOutput(job);
          const generation = ++backupRestorePollGeneration;
          await pollBackupRestore(job.id, generation);
          return true;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return null;
        }
      });
    }

    async function pollBackupRestore(id, generation) {
      for (let attempt = 0; attempt < 120; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 1000));
        if (generation !== backupRestorePollGeneration) return;
        try {
          const response = await fetch('/api/backup-restore-jobs/' + encodeURIComponent(id), { headers: authHeaders() });
          const text = await response.text();
          if (!response.ok) {
            setBackupOutput(formatAPIError(text, response.status));
            return;
          }
          const job = JSON.parse(text);
          setBackupOutput(job);
          if (job.status === 'succeeded') {
            localStorage.removeItem('veil_api_token');
            localStorage.removeItem('veil_csrf_token');
            localStorage.removeItem('veil_username');
            localStorage.removeItem('veil_user_role');
            window.location.reload();
            return;
          }
          if (job.status === 'failed') return;
        } catch (err) {
          setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
          return;
        }
      }
      setBackupOutput(veilT('status.restoreTimedOut'));
    }
`
}
