package panel

const panelBackupsCardPlaceholder = "__VEIL_PANEL_BACKUPS_CARD__"
const panelBackupsActionsPlaceholder = "__VEIL_PANEL_BACKUPS_ACTIONS__"

func panelBackupsCardHTML() string {
	return `
<div class="card">
  <h2><span class="pulse-static"></span>&nbsp;Disaster Recovery</h2>
  <p class="hint">Archives are encrypted with the server-side scheduled backup passphrase. Configure it once with <code>sudo veil backup schedule enable</code>; the browser never receives the passphrase.</p>
  <div class="form-grid">
    <div>
      <label for="backup-daily">Daily copies</label>
      <input id="backup-daily" type="number" min="0" max="365" value="7">
    </div>
    <div>
      <label for="backup-weekly">Weekly copies</label>
      <input id="backup-weekly" type="number" min="0" max="104" value="4">
    </div>
    <div>
      <label for="backup-monthly">Monthly copies</label>
      <input id="backup-monthly" type="number" min="0" max="120" value="12">
    </div>
  </div>
  <div class="actions">
    <button type="button" id="btn-create-backup" data-admin-only="true">Create encrypted backup</button>
    <button type="button" id="btn-load-backups" class="secondary">Refresh</button>
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
    function backupRetention() {
      return {
        daily: Number(document.getElementById('backup-daily').value || 0),
        weekly: Number(document.getElementById('backup-weekly').value || 0),
        monthly: Number(document.getElementById('backup-monthly').value || 0)
      };
    }

    function setBackupOutput(value) {
      const output = document.getElementById('backup-output');
      if (output) output.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
    }

    async function loadBackups() {
      const tbody = document.getElementById('backups-table-body');
      if (!tbody) return;
      tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--text-muted);"></td></tr>';
      tbody.firstElementChild.firstElementChild.textContent = veilT('backups.loading');
      try {
        const response = await fetch('/api/backups', { headers: authHeaders() });
        const text = await response.text();
        if (!response.ok) {
          tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--accent-danger);"></td></tr>';
          tbody.firstElementChild.firstElementChild.textContent = text || ('HTTP ' + response.status);
          return;
        }
        const backups = text ? JSON.parse(text) : [];
        if (!backups.length) {
          tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--text-muted);"></td></tr>';
          tbody.firstElementChild.firstElementChild.textContent = veilT('backups.empty');
          return;
        }
        tbody.innerHTML = '';
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
          verify.textContent = veilT('backups.verify');
          verify.addEventListener('click', () => verifyBackup(item.name));
          actions.appendChild(verify);
          const download = document.createElement('button');
          download.type = 'button';
          download.className = 'secondary';
          download.textContent = veilT('action.download');
          download.addEventListener('click', () => downloadBackup(item.name));
          actions.appendChild(download);
          const restore = document.createElement('button');
          restore.type = 'button';
          restore.className = 'danger';
          restore.dataset.adminOnly = 'true';
          restore.textContent = veilT('action.restore');
          restore.addEventListener('click', () => restoreBackup(item.name));
          actions.appendChild(restore);
          row.appendChild(actions);
          tbody.appendChild(row);
        });
        applyViewerRoleGuard();
      } catch (err) {
        setBackupOutput(veilT('status.loadFailed', { error: String(err) }));
      }
    }

    async function createBackup() {
      setBackupOutput(veilT('backups.creating'));
      const response = await fetch('/api/backups', {
        method: 'POST',
        headers: requestHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(Object.assign({ prune: true }, backupRetention()))
      });
      const text = await response.text();
      if (!response.ok) {
        setBackupOutput(text || ('HTTP ' + response.status));
        return;
      }
      setBackupOutput(JSON.parse(text));
      await loadBackups();
    }

    async function pruneBackups() {
      if (!confirm(veilT('confirm.pruneBackups'))) return;
      const response = await fetch('/api/backups/prune', {
        method: 'POST',
        headers: requestHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(backupRetention())
      });
      const text = await response.text();
      setBackupOutput(response.ok && text ? JSON.parse(text) : (text || ('HTTP ' + response.status)));
      if (response.ok) await loadBackups();
    }

    async function verifyBackup(name) {
      setBackupOutput(veilT('backups.verifying', { name }));
      const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/verify', {
        method: 'POST',
        headers: requestHeaders({ 'Content-Type': 'application/json' }),
        body: '{}'
      });
      const text = await response.text();
      setBackupOutput(response.ok && text ? JSON.parse(text) : (text || ('HTTP ' + response.status)));
    }

    async function downloadBackup(name) {
      const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/download', { headers: authHeaders() });
      if (!response.ok) {
        setBackupOutput(await response.text());
        return;
      }
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = name;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    }

    async function restoreBackup(name) {
      const typed = prompt(veilT('confirm.restoreBackupPrompt', { name }));
      if (typed !== name) {
        setBackupOutput(veilT('status.restoreCancelled'));
        return;
      }
      const response = await fetch('/api/backups/' + encodeURIComponent(name) + '/restore', {
        method: 'POST',
        headers: requestHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify({ confirm: true })
      });
      const text = await response.text();
      if (!response.ok) {
        setBackupOutput(text || ('HTTP ' + response.status));
        return;
      }
      const job = JSON.parse(text);
      setBackupOutput(job);
      await pollBackupRestore(job.id);
    }

    async function pollBackupRestore(id) {
      for (let attempt = 0; attempt < 120; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 1000));
        const response = await fetch('/api/backup-restore-jobs/' + encodeURIComponent(id), { headers: authHeaders() });
        const text = await response.text();
        if (!response.ok) {
          setBackupOutput(text || ('HTTP ' + response.status));
          return;
        }
        const job = JSON.parse(text);
        setBackupOutput(job);
        if (job.status === 'succeeded') {
          localStorage.removeItem('veil_csrf_token');
          localStorage.removeItem('veil_user_role');
          window.location.reload();
          return;
        }
        if (job.status === 'failed') return;
      }
      setBackupOutput(veilT('status.restoreTimedOut'));
    }
`
}
